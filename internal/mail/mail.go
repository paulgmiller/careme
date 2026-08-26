// https://app.sendgrid.com/guide/integrate/langs/go
// using SendGrid's Go Library
// https://github.com/sendgrid/sendgrid-go
package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	netmail "net/mail"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/prompts"
	"careme/internal/users"

	utypes "careme/internal/users/types"

	"github.com/samber/lo"
	"github.com/sendgrid/rest"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const mailSentPrefix = "mail/sent/"

type mailSentClaim struct {
	SentAt     time.Time `json:"sent_at"`
	UserID     string    `json:"user_id"`
	ParamsHash string    `json:"params_hash"`
}

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type emailClient interface {
	Send(message *mail.SGMailV3) (*rest.Response, error)
}

type generator interface {
	GenerateRecipes(ctx context.Context, p *recipes.GeneratorParams) (*ai.ShoppingList, error)
}

type imageGenerator interface {
	GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error)
}

type imageStore interface {
	RecipeImageExists(ctx context.Context, hash string) (bool, error)
	SaveRecipeImage(ctx context.Context, hash string, image *ai.GeneratedImage) error
}

type userStore interface {
	List(ctx context.Context) ([]utypes.User, error)
	GetByEmail(email string) (*utypes.User, error)
}

type mailer struct {
	cache              cache.Cache
	userStorage        userStore
	generator          generator // interface requires making params public
	imageGenerator     imageGenerator
	imageStore         imageStore
	locServer          locServer
	client             emailClient
	publicOrigin       string
	wait               func()
	unsubscribeFactory users.UnsubscribeTokenFactory
}

// TODO share some of this with web.go? good for mocking?
func NewMailer(cfg *config.Config) (*mailer, error) {
	cacheStore, err := cache.MakeCache()
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}
	imageCache, err := cache.EnsureCache(recipes.RecipeImagesContainer)
	if err != nil {
		return nil, fmt.Errorf("failed to create recipe image cache: %w", err)
	}

	userStorage := users.NewStorage(cacheStore)
	aiHTTPClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	mc := critique.NewManager(cfg, cacheStore, aiHTTPClient)
	ig := ingredientgrading.NewManager(cfg, cacheStore, aiHTTPClient)
	staples, err := recipes.NewCachedStaplesService(cfg, cacheStore, ig)
	if err != nil {
		return nil, fmt.Errorf("failed to create staples service: %w", err)
	}
	ss := recipes.StatusStore(cacheStore)
	aiClient := ai.NewClient(cfg.AI.APIKey, "TODOMODEL", aiHTTPClient, prompts.NewCacheRecorder(cacheStore))
	generator, err := recipes.NewGenerator(aiClient, mc, staples, ss, recipes.IO(cacheStore))
	if err != nil {
		return nil, fmt.Errorf("failed to create recipe generator: %w", err)
	}

	centroids := locations.LoadCentroids()

	locationserver, err := locations.New(cfg, cacheStore, centroids)
	if err != nil {
		return nil, fmt.Errorf("failed to create location server: %w", err)
	}

	// shove into cfg?
	sendgridkey := os.Getenv("SENDGRID_API_KEY")
	if sendgridkey == "" {
		return nil, fmt.Errorf("SENDGRID_API_KEY environment variable is not set")
	}

	return &mailer{
		cache:              cacheStore,
		userStorage:        userStorage,
		generator:          generator,
		imageGenerator:     aiClient,
		imageStore:         recipes.NewImageStore(imageCache),
		locServer:          locationserver,
		client:             sendgrid.NewSendClient(sendgridkey),
		publicOrigin:       cfg.ResolvedPublicOrigin(),
		wait:               mc.Wait,
		unsubscribeFactory: users.NewUnsubscribeTokenFactory(*cfg),
	}, nil
}

func (m *mailer) RunOnce(ctx context.Context) {
	ctx, span := otel.Tracer("careme/mail").Start(ctx, "mail_run")
	defer span.End()

	slog.InfoContext(ctx, "starting user email run")
	users, err := m.userStorage.List(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list users", "error", err.Error())
		return
	}
	span.SetAttributes(attribute.Int("mail.user_count", len(users)))

	for _, user := range users {
		m.sendEmail(ctx, user)
	}
	m.wait()
	slog.InfoContext(ctx, "finished user email run")
}

func (m *mailer) sendEmail(ctx context.Context, user utypes.User) {
	if err := m.deliverEmail(ctx, user, false); err != nil {
		slog.ErrorContext(ctx, "failed to send user email", "user", user.ID, "error", err)
	}
}

// ForceSend sends the current recipe email without checking the user's opt-in,
// shopping day, or sent-mail claim. It does not record a sent-mail claim, so an
// admin test cannot suppress the user's normally scheduled message.
func (m *mailer) ForceSend(ctx context.Context, user utypes.User) error {
	defer m.wait()

	return m.deliverEmail(ctx, user, true)
}

// ForceSendToEmail sends today's recipe email to an existing Careme user,
// targeting only the requested address even when the profile has more household
// recipients.
func (m *mailer) ForceSendToEmail(ctx context.Context, recipient string) error {
	address, err := netmail.ParseAddress(recipient)
	if err != nil {
		return fmt.Errorf("invalid recipient email %q: %w", recipient, err)
	}
	user, err := m.userStorage.GetByEmail(address.Address)
	if err != nil {
		return fmt.Errorf("find Careme user by email %q: %w", address.Address, err)
	}

	forcedUser := *user
	forcedUser.Email = []string{address.Address}
	return m.ForceSend(ctx, forcedUser)
}

func (m *mailer) deliverEmail(ctx context.Context, user utypes.User, force bool) error {
	ctx, span := otel.Tracer("careme/mail").Start(ctx, "send_email")
	defer span.End()
	ctx = logsetup.WithSessionID(ctx, "mail")
	ctx = logsetup.WithUserID(ctx, user.ID)
	span.SetAttributes(attribute.String("user.id", user.ID))

	if !force && !user.MailOptIn {
		slog.DebugContext(ctx, "user has not opted into mail", "user", user.ID)
		return nil
	}

	if len(user.Email) == 0 {
		return fmt.Errorf("user %q has no email", user.ID)
	}

	if user.FavoriteStore == "" {
		return fmt.Errorf("user %q has no favorite store", user.ID)
	}

	l, err := m.locServer.GetLocationByID(ctx, user.FavoriteStore)
	if err != nil {
		return fmt.Errorf("get location %q: %w", user.FavoriteStore, err)
	}

	date, err := recipes.StoreToDate(ctx, time.Now(), l)
	if err != nil {
		return fmt.Errorf("get timezone for location %q: %w", user.FavoriteStore, err)
	}

	uday, _ := utypes.ParseWeekday(user.ShoppingDay)

	if !force && date.Weekday() != uday {
		return nil
	}

	p := recipes.DefaultParams(l, date)
	// p.UserID = user.ID

	paramsHash := p.Hash()
	sentKey := mailSentPrefix + paramsHash + "/" + user.ID
	if !force {
		alreadySent, err := m.cache.Exists(ctx, sentKey)
		if err != nil {
			return fmt.Errorf("check sent-mail status for user %q: %w", user.ID, err)
		}
		if alreadySent {
			slog.InfoContext(ctx, "already emailed user for params hash", "user", user.ID, "params_hash", paramsHash)
			return nil
		}
	}

	rio := recipes.IO(m.cache)
	shoppingList, err := rio.FromCache(ctx, paramsHash)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			return fmt.Errorf("read shopping list %q from cache: %w", paramsHash, err)
		}

		if err := rio.SaveParams(ctx, p); err != nil {
			if !errors.Is(err, recipes.ErrAlreadyExists) {
				return fmt.Errorf("save recipe params %q: %w", paramsHash, err)
			}
		}

		// TODO refactor with recipes/server.go
		recent := lo.Filter(user.LastRecipes, func(r utypes.Recipe, _ int) bool {
			return r.CreatedAt.After(time.Now().AddDate(0, 0, -14)) // magic number. Should it be loner and shoul we use star rating?
		})
		hashes := make([]string, 0, len(recent))
		for _, recipe := range recent {
			hashes = append(hashes, recipe.Hash)
		}
		cooked := rio.FeedbackByHash(ctx, hashes)
		p.LastRecipes = lo.FilterMap(recent, func(r utypes.Recipe, _ int) (string, bool) {
			return r.Title, cooked[r.Hash].Cooked
		})
		// can orphan recipes here with crash or shutdown. Params should have a start time

		shoppingList, err = m.generator.GenerateRecipes(ctx, p)
		if err != nil {
			return fmt.Errorf("generate recipes for user %q: %w", user.ID, err)
		}
		if err := rio.SaveShoppingList(ctx, shoppingList, paramsHash); err != nil {
			return fmt.Errorf("save shopping list %q: %w", paramsHash, err)
		}
	}

	availableImages := m.prepareRecipeImages(ctx, shoppingList.Recipes)

	var buf bytes.Buffer
	unsubscribeParams := url.Values{
		"user":  []string{user.ID},
		"token": []string{m.unsubscribeFactory.UnsubscribeToken(user.ID)},
	}.Encode()
	unsubscribeURL := m.publicOrigin + "/user/unsubscribe?" + unsubscribeParams
	if err := recipes.FormatMailWithImages(p, *shoppingList, m.publicOrigin, unsubscribeURL, availableImages, &buf); err != nil {
		return fmt.Errorf("format recipe email: %w", err)
	}

	from := mail.NewEmail("Chef", "chef@careme.cooking")
	subject := recipeEmailSubject(*shoppingList)

	plainTextContent := "Check out your new recipes at " + m.publicOrigin + "/recipes?h=" + paramsHash +
		"\n\n Unsubscribe from these emails: " + unsubscribeURL

	to := mail.NewEmail(user.Email[0], user.Email[0])
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, buf.String())
	message.SetHeader("List-Unsubscribe", "<"+unsubscribeURL+">")
	message.SetHeader("List-Unsubscribe-Post", "List-Unsubscribe=One-Click")
	for _, e := range user.Email[1:] {
		p := mail.NewPersonalization()
		p.AddTos(mail.NewEmail(e, e))
		message.AddPersonalizations(p)
	}
	// client.Request, _ = sendgrid.SetDataResidency(client.Request, "eu")
	// uncomment the above line if you are sending mail using a regional EU subuser
	response, err := m.client.Send(message)
	if err != nil {
		return fmt.Errorf("send email to %q: %w", user.Email[0], err)
	}
	if response == nil {
		return fmt.Errorf("send email to %q: nil SendGrid response", user.Email[0])
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("SendGrid rejected email to %q with status %d: %s", user.Email[0], response.StatusCode, response.Body)
	}
	slog.InfoContext(ctx, "status", slog.Int("status", response.StatusCode), "body", response.Body, "headers", response.Headers)
	if force {
		return nil
	}

	sentClaim, err := json.Marshal(mailSentClaim{
		SentAt:     time.Now().UTC(),
		UserID:     user.ID,
		ParamsHash: paramsHash,
	})
	if err != nil {
		return fmt.Errorf("encode sent-mail claim for user %q: %w", user.ID, err)
	}
	if err := m.cache.Put(ctx, sentKey, string(sentClaim), cache.IfNoneMatch()); err != nil && !errors.Is(err, cache.ErrAlreadyExists) {
		return fmt.Errorf("record sent-mail claim for user %q: %w", user.ID, err)
	}

	return nil
}

func (m *mailer) prepareRecipeImages(ctx context.Context, recipeList []ai.Recipe) map[string]bool {
	imageHashes := make([]string, len(recipeList))
	var wg sync.WaitGroup
	for i, recipe := range recipeList {
		wg.Go(func() {
			if err := m.prepareRecipeImage(ctx, recipe); err != nil {
				slog.WarnContext(ctx, "recipe image omitted from email", "recipe", recipe.Title, "error", err)
				return
			}
			imageHashes[i] = recipe.ComputeHash()
		})
	}
	wg.Wait()

	availableImages := make(map[string]bool, len(imageHashes))
	for _, hash := range imageHashes {
		if hash != "" {
			availableImages[hash] = true
		}
	}
	return availableImages
}

func (m *mailer) prepareRecipeImage(ctx context.Context, recipe ai.Recipe) error {
	hash := recipe.ComputeHash()
	exists, err := m.imageStore.RecipeImageExists(ctx, hash)
	if err != nil {
		return fmt.Errorf("check image cache: %w", err)
	}
	if exists {
		return nil
	}

	generated, err := m.imageGenerator.GenerateRecipeImage(ctx, recipe)
	if err != nil {
		return fmt.Errorf("generate image: %w", err)
	}
	if err := m.imageStore.SaveRecipeImage(ctx, hash, generated); err != nil {
		return fmt.Errorf("save image: %w", err)
	}
	return nil
}

func recipeEmailSubject(shoppingList ai.ShoppingList) string {
	const (
		prefix          = "🍽️ "
		maxSubjectRunes = 60
	)
	if len(shoppingList.Recipes) == 0 {
		return prefix + "Your recipes"
	}

	title := strings.TrimSpace(shoppingList.Recipes[0].Title)
	if title == "" {
		title = "Your recipes"
	}
	suffix := ""
	if remaining := len(shoppingList.Recipes) - 1; remaining > 0 {
		suffix = fmt.Sprintf(" +%d", remaining)
	}
	available := maxSubjectRunes - utf8.RuneCountInString(prefix+suffix)
	if utf8.RuneCountInString(title) > available {
		titleRunes := []rune(title)
		title = string(titleRunes[:available-1]) + "…"
	}

	return prefix + title + suffix
}
