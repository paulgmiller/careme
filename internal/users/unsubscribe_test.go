package users

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"careme/internal/auth"
	"careme/internal/cache"
	utypes "careme/internal/users/types"
)

func TestUnsubscribeTokenValidation(t *testing.T) {
	t.Parallel()
	user := utypes.User{
		ID:    "user-123",
		Email: []string{"user@example.com"},
	}
	token := UnsubscribeToken(user)
	if !ValidUnsubscribeToken(user, token) {
		t.Fatal("expected token to validate")
	}
	if ValidUnsubscribeToken(user, token+"x") {
		t.Fatal("expected invalid token to fail validation")
	}
}

func TestHandleUnsubscribeDisablesMailOptIn(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := NewHandler(NewStorage(cacheStore), nil, auth.DefaultMock())
	u := &utypes.User{
		ID:            "u-1",
		Email:         []string{"u1@example.com"},
		FavoriteStore: "111",
		MailOptIn:     true,
	}
	if err := s.storage.Update(u); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/user/unsubscribe?"+url.Values{
		"user":  []string{u.ID},
		"token": []string{UnsubscribeToken(*u)},
	}.Encode(), nil)
	rr := httptest.NewRecorder()

	s.handleUnsubscribe(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	updated, err := s.storage.GetByID(u.ID)
	if err != nil {
		t.Fatalf("failed to load updated user: %v", err)
	}
	if updated.MailOptIn {
		t.Fatal("expected mail opt in to be disabled")
	}
}
