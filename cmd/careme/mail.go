// https://app.sendgrid.com/guide/integrate/langs/go
// using SendGrid's Go Library
// https://github.com/sendgrid/sendgrid-go
package main

import (
	"bytes"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/users"
	utypes "careme/internal/users/types"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/sendgrid/rest"
	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"
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

type mailer struct {
	cache       cache.Cache
	userStorage *users.Storage
	generator   *recipes.Generator // interface requires making params public
	locServer   locServer
	client      emailClient
}

// TODO share some of this with web.go? good for mocking?
func NewMailer(cfg *config.Config) (*mailer, error) {
	cache, err := cache.MakeCache()
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}

	userStorage := users.NewStorage(cache)

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return nil, fmt.Errorf("failed to create recipe generator: %w", err)
	}

	locationserver, err := locations.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create location server: %w", err)
	}

	// shove into cfg?
	sendgridkey := os.Getenv("SENDGRID_API_KEY")
	if sendgridkey == "" {
		return nil, fmt.Errorf("SENDGRID_API_KEY environment variable is not set")
	}

	return &mailer{
		cache:       cache,
		userStorage: userStorage,
		generator:   generator.(*recipes.Generator), // TODO do better
		locServer:   locationserver,
		client:      sendgrid.NewSendClient(sendgridkey),
	}, nil
}

func (m *mailer) RunOnce(ctx context.Context) {
	slog.InfoContext(ctx, "starting user email run")
	users, err := m.userStorage.List(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to list users", "error", err.Error())
		return
	}

	for _, user := range users {
		m.sendEmail(ctx, user)
	}
}

func (m *mailer) sendEmail(ctx context.Context, user utypes.User) {
	if !user.MailOptIn {
		slog.DebugContext(ctx, "user has not opted into mail", "user", user.ID)
		return
	}

	if len(user.Email) == 0 {
		slog.ErrorContext(ctx, "user has no email", "user", user.ID)
		return
	}

	if user.FavoriteStore == "" {
		slog.InfoContext(ctx, "no favorite store", "user", user.ID)
		return
	}

	l, err := m.locServer.GetLocationByID(ctx, user.FavoriteStore)
	if err != nil {
		slog.ErrorContext(ctx, "error getting location", "location", user.FavoriteStore, "error", err.Error())
		return
	}

	p := recipes.DefaultParams(l, time.Now().Add(-6*time.Hour)) // how do we get the timezone of the user?
	// p.UserID = user.ID
	for _, last := range user.LastRecipes {
		if last.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
			continue
		}
		p.LastRecipes = append(p.LastRecipes, last.Title)
	}

	paramsHash := p.Hash()
	sentKey := mailSentPrefix + paramsHash + "/" + user.ID
	alreadySent, err := m.cache.Exists(ctx, sentKey)
	if err != nil {
		slog.ErrorContext(ctx, "failed to check mail sent status", "user", user.ID, "params_hash", paramsHash, "error", err)
		return
	}
	if alreadySent {
		slog.InfoContext(ctx, "already emailed user for params hash", "user", user.ID, "params_hash", paramsHash)
		return
	}

	rio := recipes.IO(m.cache)
	shoppingList, err := rio.FromCache(ctx, paramsHash)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to read shopping list from cache", "user", user.ID, "params_hash", paramsHash, "error", err)
			return
		}

		if err := rio.SaveParams(ctx, p); err != nil {
			if !errors.Is(err, recipes.ErrAlreadyExists) {
				slog.ErrorContext(ctx, "failed to save params", "user", user.ID, "params_hash", paramsHash, "error", err)
				return
			}
		}

		//can orphan recipes here with crash or shutdown. Params should have a start time

		shoppingList, err = m.generator.GenerateRecipes(ctx, p)
		if err != nil {
			slog.ErrorContext(ctx, "failed to generate recipes for user", "user", user.ID, "params_hash", paramsHash, "error", err)
			return
		}
		if err := rio.SaveShoppingList(ctx, shoppingList, paramsHash); err != nil {
			slog.ErrorContext(ctx, "failed to save shopping list", "user", user.ID, "params_hash", paramsHash, "error", err)
			return
		}
	}

	var buf bytes.Buffer
	if err := recipes.FormatMail(p, *shoppingList, &buf); err != nil {
		slog.ErrorContext(ctx, "failed to format mail", "error", err)
		return
	}

	from := mail.NewEmail("Chef", "chef@careme.cooking")
	subject := "Your new recipes are ready!"

	plainTextContent := "Check out your new recipes at https://careme.cooking/recipes?h=" + paramsHash

	to := mail.NewEmail(user.Email[0], user.Email[0])
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, buf.String())
	for _, e := range user.Email[1:] {
		p := mail.NewPersonalization()
		p.AddTos(mail.NewEmail(e, e))
		message.AddPersonalizations(p)
	}
	// client.Request, _ = sendgrid.SetDataResidency(client.Request, "eu")
	// uncomment the above line if you are sending mail using a regional EU subuser
	response, err := m.client.Send(message)
	if err != nil {
		slog.ErrorContext(ctx, "mail error", "error", err.Error(), "user", user.Email[0])
		return
	}
	slog.InfoContext(ctx, "status", slog.Int("status", response.StatusCode), "body", response.Body, "headers", response.Headers)

	sentClaim, err := json.Marshal(mailSentClaim{
		SentAt:     time.Now().UTC(),
		UserID:     user.ID,
		ParamsHash: paramsHash,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to encode sent claim", "user", user.ID, "params_hash", paramsHash, "error", err)
		return
	}
	if err := m.cache.Put(ctx, sentKey, string(sentClaim), cache.IfNoneMatch()); err != nil && !errors.Is(err, cache.ErrAlreadyExists) {
		slog.ErrorContext(ctx, "failed to record sent mail claim", "user", user.ID, "params_hash", paramsHash, "error", err)
	}
}
