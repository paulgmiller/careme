package users

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	utypes "careme/internal/users/types"
)

func TestHandleUnsubscribeDisablesMailOptInOnGet(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	tf := FakeUnsubscribeTokenFactory()
	s := NewHandler(NewStorage(cacheStore), nil, auth.DefaultMock(), tf)
	u := &utypes.User{
		ID:            "u-1",
		Email:         []string{"u1@example.com"},
		FavoriteStore: "111",
		MailOptIn:     true,
		ShoppingDay:   time.Saturday.String(),
	}
	if err := s.storage.Update(u); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	params := url.Values{
		"user":  []string{u.ID},
		"token": []string{tf.UnsubscribeToken(*u)},
	}
	req := httptest.NewRequest(http.MethodGet, "/user/unsubscribe?"+params.Encode(), nil)
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

func TestHandleUnsubscribeDoesNotDisableMailOptInOnHead(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	tf := FakeUnsubscribeTokenFactory()
	s := NewHandler(NewStorage(cacheStore), nil, auth.DefaultMock(), tf)
	u := &utypes.User{
		ID:            "u-1",
		Email:         []string{"u1@example.com"},
		FavoriteStore: "111",
		MailOptIn:     true,
		ShoppingDay:   time.Saturday.String(),
	}
	if err := s.storage.Update(u); err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	req := httptest.NewRequest(http.MethodHead, "/user/unsubscribe?"+url.Values{
		"user":  []string{u.ID},
		"token": []string{tf.UnsubscribeToken(*u)},
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
	if !updated.MailOptIn {
		t.Fatal("expected HEAD unsubscribe request to leave mail opt in enabled")
	}
}
