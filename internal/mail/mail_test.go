package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/recipes"
	"careme/internal/templates"
	"careme/internal/users"
	utypes "careme/internal/users/types"

	"github.com/sendgrid/rest"
	sgmail "github.com/sendgrid/sendgrid-go/helpers/mail"
)

func TestMain(m *testing.M) {
	if err := templates.Init(&config.Config{}, "dummyhash"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

type fakeMailCache struct {
	shoppingListJSON string
	missShoppingList bool
	data             map[string]string
	existsCalls      int
}

func newFakeMailCache(t *testing.T) *fakeMailCache {
	t.Helper()
	listJSON, err := json.Marshal(ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				Title: "Test Recipe",
				Properties: ai.RecipeProperties{
					TotalMinutes:         30,
					Servings:             4,
					EstimatedCostDollars: 18,
					CaloriesPerServing:   520,
					CookingMethods:       []ai.CookingMethod{ai.CookingMethodOven},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal shopping list: %v", err)
	}
	return &fakeMailCache{
		shoppingListJSON: string(listJSON),
		data:             map[string]string{},
	}
}

func (c *fakeMailCache) Get(_ context.Context, key string) (io.ReadCloser, error) {
	if strings.HasPrefix(key, "shoppinglist/") {
		if c.missShoppingList {
			return nil, cache.ErrNotFound
		}
		return io.NopCloser(strings.NewReader(c.shoppingListJSON)), nil
	}
	value, ok := c.data[key]
	if !ok {
		return nil, cache.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(value)), nil
}

func (c *fakeMailCache) Exists(_ context.Context, key string) (bool, error) {
	c.existsCalls++
	_, ok := c.data[key]
	return ok, nil
}

func (c *fakeMailCache) Put(_ context.Context, key, value string, opts cache.PutOptions) error {
	if opts.Condition == cache.PutIfNoneMatch {
		if _, exists := c.data[key]; exists {
			return cache.ErrAlreadyExists
		}
	}
	c.data[key] = value
	return nil
}

func (c *fakeMailCache) PutReader(_ context.Context, key string, reader io.Reader, opts cache.PutOptions) error {
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return c.Put(context.Background(), key, string(body), opts)
}

type fakeMailLocServer struct {
	location *locations.Location
}

func (f *fakeMailLocServer) GetLocationByID(_ context.Context, _ string) (*locations.Location, error) {
	return f.location, nil
}

type fakeMailClient struct {
	response *rest.Response
	err      error
	last     *sgmail.SGMailV3
}

type fakeMailUserStore struct {
	user        *utypes.User
	lookupEmail string
	err         error
}

func (s *fakeMailUserStore) List(_ context.Context) ([]utypes.User, error) {
	if s.user == nil {
		return nil, s.err
	}
	return []utypes.User{*s.user}, s.err
}

func (s *fakeMailUserStore) GetByEmail(email string) (*utypes.User, error) {
	s.lookupEmail = email
	return s.user, s.err
}

func (f *fakeMailClient) Send(msg *sgmail.SGMailV3) (*rest.Response, error) {
	f.last = msg
	return f.response, f.err
}

type capturingMailGenerator struct {
	ctx context.Context
}

type fakeMailImageGenerator struct{}

func (fakeMailImageGenerator) GenerateRecipeImage(_ context.Context, _ ai.Recipe) (*ai.GeneratedImage, error) {
	return &ai.GeneratedImage{Body: bytes.NewReader([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})}, nil
}

func configureFakeMailImages(m *mailer) {
	m.imageGenerator = fakeMailImageGenerator{}
	m.imageStore = recipes.NewImageStore(cache.NewInMemoryCache())
}

func (g *capturingMailGenerator) GenerateRecipes(ctx context.Context, _ *recipes.GeneratorParams) (*ai.ShoppingList, error) {
	g.ctx = ctx
	return &ai.ShoppingList{
		Recipes: []ai.Recipe{
			{Title: "Generated Test Recipe"},
		},
	}, nil
}

func shoppingDayForStore(t *testing.T, location *locations.Location) string {
	t.Helper()
	date, err := recipes.StoreToDate(context.Background(), time.Now(), location)
	if err != nil {
		t.Fatalf("failed to resolve store date: %v", err)
	}
	return date.Weekday().String()
}

func testMailLocation() *locations.Location {
	lat := 47.61
	lon := -122.33
	return &locations.Location{
		ID:      "123",
		Name:    "Test Store",
		Address: "123 Test St",
		ZipCode: "98005",
		Lat:     &lat,
		Lon:     &lon,
	}
}

func TestSendEmail_DoesNotRecordSentClaimOnNonSuccessSendGridStatus(t *testing.T) {
	fc := newFakeMailCache(t)
	location := testMailLocation()
	m := &mailer{
		cache: fc,
		locServer: &fakeMailLocServer{
			location: location,
		},
		client: &fakeMailClient{
			response: &rest.Response{StatusCode: 500, Body: "sendgrid internal error"},
		},
		unsubscribeFactory: users.FakeUnsubscribeTokenFactory(),
	}
	configureFakeMailImages(m)

	m.sendEmail(context.Background(), utypes.User{
		ID:            "user-1",
		MailOptIn:     true,
		Email:         []string{"u1@example.com"},
		FavoriteStore: "123",
		ShoppingDay:   shoppingDayForStore(t, location),
	})

	for key := range fc.data {
		if strings.HasPrefix(key, mailSentPrefix) {
			t.Fatalf("did not expect sent claim to be recorded for non-success status; got key %q", key)
		}
	}
}

func TestSendEmail_RecordsSentClaimOnSuccessSendGridStatus(t *testing.T) {
	fc := newFakeMailCache(t)
	location := testMailLocation()
	client := &fakeMailClient{
		response: &rest.Response{StatusCode: 202, Body: "accepted"},
	}
	m := &mailer{
		cache: fc,
		locServer: &fakeMailLocServer{
			location: location,
		},
		client:             client,
		publicOrigin:       "https://careme.cooking",
		unsubscribeFactory: users.FakeUnsubscribeTokenFactory(),
	}
	configureFakeMailImages(m)

	m.sendEmail(context.Background(), utypes.User{
		ID:            "user-1",
		MailOptIn:     true,
		Email:         []string{"u1@example.com"},
		FavoriteStore: "123",
		ShoppingDay:   shoppingDayForStore(t, location),
	})

	var (
		foundKey   string
		claimValue string
	)
	for key, value := range fc.data {
		if strings.HasPrefix(key, mailSentPrefix) {
			foundKey = key
			claimValue = value
			break
		}
	}
	if foundKey == "" {
		t.Fatalf("expected sent claim to be recorded for successful status")
	}
	if !strings.HasSuffix(foundKey, "/user-1") {
		t.Fatalf("expected sent claim key to end with /user-1, got %q", foundKey)
	}

	var claim mailSentClaim
	if err := json.Unmarshal([]byte(claimValue), &claim); err != nil {
		t.Fatalf("failed to decode sent claim: %v", err)
	}
	if claim.UserID != "user-1" {
		t.Fatalf("expected claim user id user-1, got %q", claim.UserID)
	}
	if claim.ParamsHash == "" {
		t.Fatalf("expected claim params hash to be set")
	}
	if client.last == nil || !strings.Contains(client.last.Content[1].Value, "Unsubscribe") {
		t.Fatalf("expected sent message to contain unsubscribe link %s", client.last.Content[1].Value)
	}
	wantOneClickURL := "<https://careme.cooking/user/unsubscribe?token=" +
		users.FakeUnsubscribeTokenFactory().UnsubscribeToken("user-1") + "&user=user-1>"
	if got := client.last.Headers["List-Unsubscribe"]; got != wantOneClickURL {
		t.Fatalf("expected List-Unsubscribe header %q, got %q", wantOneClickURL, got)
	}
	if got := client.last.Headers["List-Unsubscribe-Post"]; got != "List-Unsubscribe=One-Click" {
		t.Fatalf("expected one-click List-Unsubscribe-Post header, got %q", got)
	}
	if got := client.last.Subject; got != "🍽️ Test Recipe" {
		t.Fatalf("expected dynamic recipe subject, got %q", got)
	}
	if len(client.last.Attachments) != 0 {
		t.Fatalf("expected no image attachments, got %d", len(client.last.Attachments))
	}
	htmlContent := client.last.Content[1].Value
	recipe := ai.Recipe{
		Title: "Test Recipe",
		Properties: ai.RecipeProperties{
			TotalMinutes:         30,
			Servings:             4,
			EstimatedCostDollars: 18,
			CaloriesPerServing:   520,
			CookingMethods:       []ai.CookingMethod{ai.CookingMethodOven},
		},
	}
	recipeHash := recipe.ComputeHash()
	for _, want := range []string{"https://careme.cooking/recipe/" + recipeHash + "/image", "⏱️", "30 min", "👥&nbsp;4</span>", "💵", "$18", "❤️", "520 cal", "♨️", "Oven"} {
		if !strings.Contains(htmlContent, want) {
			t.Fatalf("expected email HTML to contain %q", want)
		}
	}
}

func TestSendEmail_GenerationContextIncludesMailSessionAndUserID(t *testing.T) {
	fc := newFakeMailCache(t)
	fc.missShoppingList = true
	location := testMailLocation()
	generator := &capturingMailGenerator{}
	m := &mailer{
		cache: fc,
		locServer: &fakeMailLocServer{
			location: location,
		},
		generator: generator,
		client: &fakeMailClient{
			response: &rest.Response{StatusCode: 202, Body: "accepted"},
		},
		publicOrigin:       "https://careme.cooking",
		unsubscribeFactory: users.FakeUnsubscribeTokenFactory(),
	}
	configureFakeMailImages(m)

	m.sendEmail(context.Background(), utypes.User{
		ID:            "user-1",
		MailOptIn:     true,
		Email:         []string{"u1@example.com"},
		FavoriteStore: "123",
		ShoppingDay:   shoppingDayForStore(t, location),
	})

	if generator.ctx == nil {
		t.Fatal("expected generator to be called")
	}
	sessionID, ok := logsetup.SessionIDFromContext(generator.ctx)
	if !ok {
		t.Fatal("expected session id in generator context")
	}
	if sessionID != "mail" {
		t.Fatalf("expected session id mail, got %q", sessionID)
	}
	userID, ok := logsetup.UserIDFromContext(generator.ctx)
	if !ok {
		t.Fatal("expected user id in generator context")
	}
	if userID != "user-1" {
		t.Fatalf("expected user id user-1, got %q", userID)
	}
}

func TestForceSendBypassesPreferencesAndDoesNotRecordSentClaim(t *testing.T) {
	fc := newFakeMailCache(t)
	location := testMailLocation()
	client := &fakeMailClient{
		response: &rest.Response{StatusCode: 202, Body: "accepted"},
	}
	waited := false
	m := &mailer{
		cache: fc,
		locServer: &fakeMailLocServer{
			location: location,
		},
		client:             client,
		publicOrigin:       "https://careme.cooking",
		wait:               func() { waited = true },
		unsubscribeFactory: users.FakeUnsubscribeTokenFactory(),
	}
	configureFakeMailImages(m)

	err := m.ForceSend(context.Background(), utypes.User{
		ID:            "user-1",
		MailOptIn:     false,
		Email:         []string{"u1@example.com"},
		FavoriteStore: "123",
		ShoppingDay:   "not today",
	})
	if err != nil {
		t.Fatalf("ForceSend() error = %v", err)
	}
	if client.last == nil {
		t.Fatal("expected forced email to be sent")
	}
	if fc.existsCalls != 0 {
		t.Fatalf("expected forced email to skip sent-mail lookup, got %d lookups", fc.existsCalls)
	}
	for key := range fc.data {
		if strings.HasPrefix(key, mailSentPrefix) {
			t.Fatalf("did not expect forced email to record sent claim; got key %q", key)
		}
	}
	if !waited {
		t.Fatal("expected ForceSend to wait for background recipe work")
	}
}

func TestForceSendToEmailUsesProfileAndTargetsOnlyRequestedAddress(t *testing.T) {
	fc := newFakeMailCache(t)
	location := testMailLocation()
	client := &fakeMailClient{
		response: &rest.Response{StatusCode: 202, Body: "accepted"},
	}
	store := &fakeMailUserStore{user: &utypes.User{
		ID:            "user-1",
		MailOptIn:     false,
		Email:         []string{"u1@example.com", "household@example.com"},
		FavoriteStore: "123",
		ShoppingDay:   "not today",
	}}
	m := &mailer{
		cache:       fc,
		userStorage: store,
		locServer: &fakeMailLocServer{
			location: location,
		},
		client:             client,
		publicOrigin:       "https://careme.cooking",
		wait:               func() {},
		unsubscribeFactory: users.FakeUnsubscribeTokenFactory(),
	}
	configureFakeMailImages(m)

	err := m.ForceSendToEmail(context.Background(), "Paul <u1@example.com>")
	if err != nil {
		t.Fatalf("ForceSendToEmail() error = %v", err)
	}
	if store.lookupEmail != "u1@example.com" {
		t.Fatalf("expected normalized parsed lookup email, got %q", store.lookupEmail)
	}
	if client.last == nil {
		t.Fatal("expected recipe email to be sent")
	}
	if len(client.last.Personalizations) != 1 || len(client.last.Personalizations[0].To) != 1 {
		t.Fatalf("expected one recipient, got %#v", client.last.Personalizations)
	}
	if got := client.last.Personalizations[0].To[0].Address; got != "u1@example.com" {
		t.Fatalf("expected only requested recipient, got %q", got)
	}
}

func TestRecipeEmailSubjectIsDynamicAndShort(t *testing.T) {
	tests := []struct {
		name     string
		recipes  []ai.Recipe
		expected string
	}{
		{
			name:     "no recipes",
			expected: "🍽️ Your recipes",
		},
		{
			name:     "one recipe",
			recipes:  []ai.Recipe{{Title: "Chicken piccata"}},
			expected: "🍽️ Chicken piccata",
		},
		{
			name: "multiple recipes",
			recipes: []ai.Recipe{
				{Title: "Chicken piccata"},
				{Title: "Spring pasta"},
				{Title: "Salmon bowls"},
			},
			expected: "🍽️ Chicken piccata +2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recipeEmailSubject(ai.ShoppingList{Recipes: tt.recipes})
			if got != tt.expected {
				t.Fatalf("recipeEmailSubject() = %q, want %q", got, tt.expected)
			}
			if utf8.RuneCountInString(got) > 60 {
				t.Fatalf("subject has %d runes, want at most 60: %q", utf8.RuneCountInString(got), got)
			}
		})
	}

	longTitle := strings.Repeat("Delicious dinner ", 10)
	got := recipeEmailSubject(ai.ShoppingList{Recipes: []ai.Recipe{{Title: longTitle}, {Title: "Second"}}})
	if utf8.RuneCountInString(got) != 60 {
		t.Fatalf("truncated subject has %d runes, want 60: %q", utf8.RuneCountInString(got), got)
	}
	if !strings.HasSuffix(got, "… +1") {
		t.Fatalf("expected truncated subject to preserve remaining count, got %q", got)
	}
}
