package users

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	utypes "careme/internal/users/types"
)

type stubEmailFetcher struct {
	email string
	err   error
	calls int
}

func (s *stubEmailFetcher) GetUserEmail(_ context.Context, _ string) (string, error) {
	s.calls++
	return s.email, s.err
}

func TestStorageUpdateAndGetByID(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	user := &utypes.User{
		ID:          "user-1",
		Email:       []string{"Alice@Example.com"},
		ShoppingDay: time.Monday.String(),
	}

	if err := storage.Update(user); err != nil {
		t.Fatalf("Update() error: %v", err)
	}

	got, err := storage.GetByID("user-1")
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("GetByID() ID = %q, want %q", got.ID, user.ID)
	}
	if got.ShoppingDay != user.ShoppingDay {
		t.Fatalf("GetByID() ShoppingDay = %q, want %q", got.ShoppingDay, user.ShoppingDay)
	}
	if len(got.Email) != 1 || got.Email[0] != user.Email[0] {
		t.Fatalf("GetByID() Email = %v, want %v", got.Email, user.Email)
	}
}

func TestStorageGetByIDNotFound(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	_, err := storage.GetByID("missing")
	if err == nil || err != ErrNotFound {
		t.Fatalf("GetByID() error = %v, want %v", err, ErrNotFound)
	}
}

func TestStorageGetByEmailNotFound(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	_, err := storage.GetByEmail("missing@example.com")
	if err == nil || err != ErrNotFound {
		t.Fatalf("GetByEmail() error = %v, want %v", err, ErrNotFound)
	}
}

func TestStorageGetByEmail(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	user := &utypes.User{
		ID:          "user-2",
		Email:       []string{"Alice@Example.com"},
		ShoppingDay: time.Tuesday.String(),
	}

	if err := storage.Update(user); err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if err := fc.Put(context.Background(), emailPrefix+normalizeEmail(user.Email[0]), user.ID, cache.Unconditional()); err != nil {
		t.Fatalf("failed to index email: %v", err)
	}

	got, err := storage.GetByEmail("ALICE@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("GetByEmail() error: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("GetByEmail() ID = %q, want %q", got.ID, user.ID)
	}
}

func TestFindOrCreateFromClerkExistingUser(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	user := &utypes.User{
		ID:          "user-3",
		Email:       []string{"dana@example.com"},
		ShoppingDay: time.Wednesday.String(),
	}
	if err := storage.Update(user); err != nil {
		t.Fatalf("Update() error: %v", err)
	}

	fetcher := &stubEmailFetcher{email: "should-not-call@example.com"}
	got, err := storage.FindOrCreateFromClerk(context.Background(), "user-3", fetcher)
	if err != nil {
		t.Fatalf("FindOrCreateFromClerk() error: %v", err)
	}
	if fetcher.calls != 0 {
		t.Fatalf("expected email fetcher to not be called, calls=%d", fetcher.calls)
	}
	if got.ID != user.ID {
		t.Fatalf("FindOrCreateFromClerk() ID = %q, want %q", got.ID, user.ID)
	}
}

func TestFindOrCreateFromClerkCreatesUser(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	fetcher := &stubEmailFetcher{email: "NewUser@Example.com"}
	start := time.Now()
	got, err := storage.FindOrCreateFromClerk(context.Background(), "user-4", fetcher)
	end := time.Now()
	if err != nil {
		t.Fatalf("FindOrCreateFromClerk() error: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("expected email fetcher to be called once, calls=%d", fetcher.calls)
	}
	if got.ID != "user-4" {
		t.Fatalf("FindOrCreateFromClerk() ID = %q, want %q", got.ID, "user-4")
	}
	if len(got.Email) != 1 || got.Email[0] != "newuser@example.com" {
		t.Fatalf("FindOrCreateFromClerk() Email = %v, want [newuser@example.com]", got.Email)
	}
	if got.ShoppingDay != time.Saturday.String() {
		t.Fatalf("FindOrCreateFromClerk() ShoppingDay = %q, want %q", got.ShoppingDay, time.Saturday.String())
	}
	if got.CreatedAt.Before(start) || got.CreatedAt.After(end) {
		t.Fatalf("FindOrCreateFromClerk() CreatedAt = %v, expected between %v and %v", got.CreatedAt, start, end)
	}

	stored, err := storage.GetByID("user-4")
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if stored.ID != got.ID {
		t.Fatalf("stored ID = %q, want %q", stored.ID, got.ID)
	}
}

func TestFromRequestCreatesUserWithMockAuth(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	cfg := &config.Config{
		Mocks: config.MockConfig{Email: "NewUser@Example.com"},
	}
	client := auth.Mock(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	got, err := storage.FromRequest(context.Background(), req, client)
	if err != nil {
		t.Fatalf("FromRequest() error: %v", err)
	}
	if got.ID != "mock-clerk-user-id" {
		t.Fatalf("FromRequest() ID = %q, want %q", got.ID, "mock-clerk-user-id")
	}
	if len(got.Email) != 1 || got.Email[0] != "newuser@example.com" {
		t.Fatalf("FromRequest() Email = %v, want [newuser@example.com]", got.Email)
	}
}

func TestFromRequestReturnsExistingUser(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	existing := &utypes.User{
		ID:          "mock-clerk-user-id",
		Email:       []string{"existing@example.com"},
		ShoppingDay: time.Thursday.String(),
	}
	if err := storage.Update(existing); err != nil {
		t.Fatalf("Update() error: %v", err)
	}

	cfg := &config.Config{
		Mocks: config.MockConfig{Email: "ignored@example.com"},
	}
	client := auth.Mock(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	got, err := storage.FromRequest(context.Background(), req, client)
	if err != nil {
		t.Fatalf("FromRequest() error: %v", err)
	}
	if got.ID != existing.ID {
		t.Fatalf("FromRequest() ID = %q, want %q", got.ID, existing.ID)
	}
	if len(got.Email) != 1 || got.Email[0] != "existing@example.com" {
		t.Fatalf("FromRequest() Email = %v, want [existing@example.com]", got.Email)
	}
}
