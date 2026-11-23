package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	if err == nil || err != errNotFound {
		t.Fatalf("GetByID() error = %v, want %v", err, errNotFound)
	}
}

func TestStorageUpdateKeepsNewestShoppingListForTwoStoreNames(t *testing.T) {
	t.Parallel()
	storage := NewStorage(cache.NewFileCache(t.TempDir()))
	now := time.Now().Round(0)
	user := &utypes.User{
		ID:          "user-shopping-lists",
		Email:       []string{"chef@example.com"},
		ShoppingDay: time.Saturday.String(),
		ShoppingLists: []utypes.ShoppingList{
			{Hash: "old-market-list", Name: "Market", CompletedAt: now.Add(-6 * 24 * time.Hour)},
			{Hash: "grocer-list", Name: "Grocer", CompletedAt: now.Add(-2 * time.Hour)},
			{Hash: "new-market-list", Name: "market", CompletedAt: now},
			{Hash: "third-list", Name: "Third Store", CompletedAt: now.Add(time.Hour)},
		},
	}
	require.NoError(t, storage.Update(user))

	user, err := storage.GetByID("user-shopping-lists")
	require.NoError(t, err)
	require.Len(t, user.ShoppingLists, 2)
	assert.Equal(t, []string{"Third Store", "market"}, []string{user.ShoppingLists[0].Name, user.ShoppingLists[1].Name})
	assert.Equal(t, []string{"third-list", "new-market-list"}, []string{user.ShoppingLists[0].Hash, user.ShoppingLists[1].Hash})
}

func TestStorageUpdatePrunesShoppingListsOlderThanSevenDays(t *testing.T) {
	t.Parallel()
	storage := NewStorage(cache.NewFileCache(t.TempDir()))
	now := time.Now()
	user := &utypes.User{
		ID:          "user-prune-shopping-lists",
		Email:       []string{"chef@example.com"},
		ShoppingDay: time.Saturday.String(),
		ShoppingLists: []utypes.ShoppingList{
			{Hash: "expired", Name: "Old Store", CompletedAt: now.Add(-8 * 24 * time.Hour)},
			{Hash: "current", Name: "Current Store", CompletedAt: now.Add(-6 * 24 * time.Hour)},
		},
	}
	require.NoError(t, storage.Update(user))
	require.Len(t, user.ShoppingLists, 1)
	assert.Equal(t, "current", user.ShoppingLists[0].Hash)

	stored, err := storage.GetByID(user.ID)
	require.NoError(t, err)
	require.Len(t, stored.ShoppingLists, 1)
	assert.Equal(t, user.ShoppingLists[0].Hash, stored.ShoppingLists[0].Hash)
	assert.True(t, user.ShoppingLists[0].CompletedAt.Equal(stored.ShoppingLists[0].CompletedAt))
}

func TestNormalizeShoppingListsKeepsExactSevenDayBoundary(t *testing.T) {
	t.Parallel()
	now := time.Now().Round(0)
	user := &utypes.User{ShoppingLists: []utypes.ShoppingList{
		{Hash: "expired", Name: "Old Store", CompletedAt: now.Add(-7*24*time.Hour - time.Nanosecond)},
		{Hash: "boundary", Name: "Current Store", CompletedAt: now.Add(-7 * 24 * time.Hour)},
	}}

	require.NoError(t, normalizeShoppingLists(user, now))
	require.Len(t, user.ShoppingLists, 1)
	assert.Equal(t, "boundary", user.ShoppingLists[0].Hash)
}

func TestStorageGetByEmailNotFound(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	_, err := storage.GetByEmail("missing@example.com")
	if err == nil || err != errNotFound {
		t.Fatalf("GetByEmail() error = %v, want %v", err, errNotFound)
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
	got, err := storage.findOrCreateFromClerk(context.Background(), "user-3", fetcher)
	if err != nil {
		t.Fatalf("findOrCreateFromClerk() error: %v", err)
	}
	if fetcher.calls != 0 {
		t.Fatalf("expected email fetcher to not be called, calls=%d", fetcher.calls)
	}
	if got.ID != user.ID {
		t.Fatalf("findOrCreateFromClerk() ID = %q, want %q", got.ID, user.ID)
	}
}

func TestFindOrCreateFromClerkCreatesUser(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	fetcher := &stubEmailFetcher{email: "NewUser@Example.com"}
	start := time.Now()
	got, err := storage.findOrCreateFromClerk(context.Background(), "user-4", fetcher)
	end := time.Now()
	if err != nil {
		t.Fatalf("findOrCreateFromClerk() error: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("expected email fetcher to be called once, calls=%d", fetcher.calls)
	}
	if got.ID != "user-4" {
		t.Fatalf("findOrCreateFromClerk() ID = %q, want %q", got.ID, "user-4")
	}
	if len(got.Email) != 1 || got.Email[0] != "newuser@example.com" {
		t.Fatalf("findOrCreateFromClerk() Email = %v, want [newuser@example.com]", got.Email)
	}
	if got.ShoppingDay != time.Saturday.String() {
		t.Fatalf("findOrCreateFromClerk() ShoppingDay = %q, want %q", got.ShoppingDay, time.Saturday.String())
	}
	if got.CreatedAt.Before(start) || got.CreatedAt.After(end) {
		t.Fatalf("findOrCreateFromClerk() CreatedAt = %v, expected between %v and %v", got.CreatedAt, start, end)
	}

	stored, err := storage.GetByID("user-4")
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	if stored.ID != got.ID {
		t.Fatalf("stored ID = %q, want %q", stored.ID, got.ID)
	}
}

func TestFromRequestWithoutSession(t *testing.T) {
	t.Parallel()

	storage := NewStorage(cache.NewInMemoryCache())
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	user, err := storage.FromRequest(t.Context(), r, noSessionAuth{})
	require.ErrorIs(t, err, auth.ErrNoSession)
	require.Nil(t, user)
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

func TestStorageRemoveRecipesByHash(t *testing.T) {
	fc := cache.NewFileCache(t.TempDir())
	storage := NewStorage(fc)

	keep := utypes.Recipe{
		Title:     "Keep",
		Hash:      "keep-hash",
		CreatedAt: time.Now().Add(-time.Hour).Round(0),
	}
	removeOne := utypes.Recipe{
		Title:     "Remove One",
		Hash:      "remove-hash",
		CreatedAt: time.Now().Add(-30 * time.Minute).Round(0),
	}
	removeTwo := utypes.Recipe{
		Title:     "Remove Two",
		Hash:      "remove-hash",
		CreatedAt: time.Now().Round(0),
	}
	user := &utypes.User{
		ID:          "user-remove-hash",
		Email:       []string{"hash@example.com"},
		ShoppingDay: time.Saturday.String(),
		LastRecipes: []utypes.Recipe{keep, removeOne, removeTwo},
	}
	if err := storage.Update(user); err != nil {
		t.Fatalf("Update() error: %v", err)
	}

	removed, err := storage.removeRecipe(user, "remove-hash")
	if err != nil {
		t.Fatalf("RemoveRecipe() error: %v", err)
	}
	if !removed {
		t.Fatalf("RemoveRecipe() removed = false, want true")
	}
	if len(user.LastRecipes) != 1 || user.LastRecipes[0].Hash != keep.Hash {
		t.Fatalf("RemoveRecipe() LastRecipes = %#v, want only keep recipe", user.LastRecipes)
	}
}

func TestRecipeHistoryTransitions(t *testing.T) {
	t.Parallel()
	storage := NewStorage(cache.NewInMemoryCache())
	now := time.Now().UTC().Round(0)
	old := utypes.Recipe{Title: "Old saved recipe", Hash: "old", CreatedAt: now.Add(-90 * 24 * time.Hour)}
	user := &utypes.User{ID: "history", ShoppingDay: time.Saturday.String(), Email: []string{"history@example.com"}, LastRecipes: []utypes.Recipe{old}}
	for _, saved := range []bool{true, true, false, false, true} {
		recipe := utypes.Recipe{Title: "Pasta", Hash: "pasta", CreatedAt: now}
		require.NoError(t, storage.SetRecipeSaved(user, recipe, saved))
		loaded, err := storage.GetByID(user.ID)
		require.NoError(t, err)
		if saved {
			assert.Equal(t, []utypes.Recipe{recipe, old}, loaded.LastRecipes)
			assert.Empty(t, loaded.DismissedRecipes)
		} else {
			assert.Equal(t, []utypes.Recipe{old}, loaded.LastRecipes)
			assert.Equal(t, []utypes.Recipe{recipe}, loaded.DismissedRecipes)
		}
		user = loaded
		now = now.Add(time.Second)
	}
}

func TestDismissedHistoryPruning(t *testing.T) {
	t.Parallel()
	now := time.Now()
	old := utypes.Recipe{Title: "Old", Hash: "old", CreatedAt: now.Add(-15 * 24 * time.Hour)}
	recent := utypes.Recipe{Title: "Recent", Hash: "recent", CreatedAt: now.Add(-8 * 24 * time.Hour)}
	user := &utypes.User{
		ID: "pruning", ShoppingDay: time.Saturday.String(), Email: []string{"pruning@example.com"},
		LastRecipes: []utypes.Recipe{old}, DismissedRecipes: []utypes.Recipe{old, recent},
	}
	c := cache.NewInMemoryCache()
	storage := NewStorage(c)
	// Seed a legacy user directly so the read must prune expired dismissals.
	body, err := json.Marshal(user)
	require.NoError(t, err)
	require.NoError(t, c.Put(t.Context(), userPrefix+user.ID, string(body), cache.Unconditional()))
	loaded, err := storage.GetByID(user.ID)
	require.NoError(t, err)
	require.Len(t, loaded.LastRecipes, 1, "old saved recipes are retained")
	require.Len(t, loaded.DismissedRecipes, 1)
	assert.Equal(t, recent.Hash, loaded.DismissedRecipes[0].Hash)
	// Writes also discard expired history from an already loaded user.
	require.NoError(t, storage.Update(user))
	require.Len(t, user.DismissedRecipes, 1)
	assert.Equal(t, recent.Hash, user.DismissedRecipes[0].Hash)
}

func TestDismissedHistoryRetentionBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-2 * utypes.RecipeCooldownWindow)
	user := &utypes.User{DismissedRecipes: []utypes.Recipe{
		{Hash: "inside", CreatedAt: cutoff.Add(time.Nanosecond)},
		{Hash: "boundary", CreatedAt: cutoff},
		{Hash: "outside", CreatedAt: cutoff.Add(-time.Nanosecond)},
	}}
	pruneDismissedRecipes(user, now)
	require.Len(t, user.DismissedRecipes, 1)
	assert.Equal(t, "inside", user.DismissedRecipes[0].Hash)
}
