package mail

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/templates"
	utypes "careme/internal/users/types"

	"github.com/sendgrid/rest"
	sgmail "github.com/sendgrid/sendgrid-go/helpers/mail"
)

type fakeMailCache struct {
	shoppingListJSON string
	data             map[string]string
}

func newFakeMailCache(t *testing.T) *fakeMailCache {
	t.Helper()
	listJSON, err := json.Marshal(ai.ShoppingList{
		Recipes: []ai.Recipe{
			{Title: "Test Recipe"},
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
		return io.NopCloser(strings.NewReader(c.shoppingListJSON)), nil
	}
	value, ok := c.data[key]
	if !ok {
		return nil, cache.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(value)), nil
}

func (c *fakeMailCache) Exists(_ context.Context, key string) (bool, error) {
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

type fakeMailLocServer struct {
	location *locations.Location
}

func (f *fakeMailLocServer) GetLocationByID(_ context.Context, _ string) (*locations.Location, error) {
	return f.location, nil
}

type fakeMailClient struct {
	response *rest.Response
	err      error
}

func (f *fakeMailClient) Send(_ *sgmail.SGMailV3) (*rest.Response, error) {
	return f.response, f.err
}

func TestSendEmail_DoesNotRecordSentClaimOnNonSuccessSendGridStatus(t *testing.T) {
	if err := templates.Init(&config.Config{}, "/assets/tailwind.css"); err != nil {
		t.Fatalf("failed to initialize templates: %v", err)
	}

	fc := newFakeMailCache(t)
	m := &mailer{
		cache: fc,
		locServer: &fakeMailLocServer{
			location: &locations.Location{ID: "123", Name: "Test Store", Address: "123 Test St"},
		},
		client: &fakeMailClient{
			response: &rest.Response{StatusCode: 500, Body: "sendgrid internal error"},
		},
	}

	m.sendEmail(context.Background(), utypes.User{
		ID:            "user-1",
		MailOptIn:     true,
		Email:         []string{"u1@example.com"},
		FavoriteStore: "123",
	})

	for key := range fc.data {
		if strings.HasPrefix(key, mailSentPrefix) {
			t.Fatalf("did not expect sent claim to be recorded for non-success status; got key %q", key)
		}
	}
}

func TestSendEmail_RecordsSentClaimOnSuccessSendGridStatus(t *testing.T) {
	if err := templates.Init(&config.Config{}, "/assets/tailwind.css"); err != nil {
		t.Fatalf("failed to initialize templates: %v", err)
	}

	fc := newFakeMailCache(t)
	m := &mailer{
		cache: fc,
		locServer: &fakeMailLocServer{
			location: &locations.Location{ID: "123", Name: "Test Store", Address: "123 Test St", ZipCode: "98005"},
		},
		client: &fakeMailClient{
			response: &rest.Response{StatusCode: 202, Body: "accepted"},
		},
	}

	m.sendEmail(context.Background(), utypes.User{
		ID:            "user-1",
		MailOptIn:     true,
		Email:         []string{"u1@example.com"},
		FavoriteStore: "123",
		ShoppingDay:   time.Now().Weekday().String(),
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
}

//TODO tests for optin and day of week?
