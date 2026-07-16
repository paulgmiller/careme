package recipes

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/guest"
	"careme/internal/locations"
	"careme/internal/recipes/feedback"
	recipestatus "careme/internal/recipes/status"
	"careme/internal/routing"
	"careme/internal/users"

	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedirectToHash(t *testing.T) {
	// Create a ResponseRecorder to record the response
	rr := httptest.NewRecorder()
	// Create a dummy request
	req := httptest.NewRequest("GET", "/dummy", nil)

	hash := "testhash"
	redirectToHash(rr, req, hash, queryArgStart)

	// Check the status code
	if status := rr.Code; status != http.StatusSeeOther {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusSeeOther)
	}

	// Check the Location header
	expectedLocation := fmt.Sprintf("/recipes?h=%s&start=", hash)
	location := rr.Header().Get("Location")
	if !strings.HasPrefix(location, expectedLocation) {
		t.Errorf("handler returned wrong location: got %v want prefix %v", location, expectedLocation)
	}
}

func TestRedirectToHashWithHelpKeepsHelpAsQueryOnly(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/recipes?location=store-1&help=Save+two+dinners", nil)

	redirectToHash(rr, req, "testhash", queryArgStart, QueryArgHelp)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	location := rr.Header().Get("Location")
	u, err := url.Parse(location)
	require.NoError(t, err)
	assert.Equal(t, "/recipes", u.Path)
	assert.Equal(t, "testhash", u.Query().Get("h"))
	assert.NotEmpty(t, u.Query().Get("start"))
	assert.Equal(t, "Save two dinners", u.Query().Get("help"))
}

func legacyRecipeHash(hash string) (string, bool) {
	return currentHashToLegacy(hash, legacyRecipeHashSeed)
}

func currentHashToLegacy(hash string, seed string) (string, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(hash)
	if err != nil || len(decoded) == 0 {
		return "", false
	}
	seedBytes := []byte(seed)
	if bytes.HasPrefix(decoded, seedBytes) {
		return hash, false
	}
	legacyDecoded := make([]byte, 0, len(seedBytes)+len(decoded))
	legacyDecoded = append(legacyDecoded, seedBytes...)
	legacyDecoded = append(legacyDecoded, decoded...)
	return base64.URLEncoding.EncodeToString(legacyDecoded), true
}

func TestHandleRecipes_RedirectsLegacyHashToCanonicalHash(t *testing.T) {
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Test"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))
	hash := p.Hash()
	legacyHash, ok := legacyRecipeHash(hash)
	if !ok {
		t.Fatal("expected to derive legacy recipe hash")
	}

	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+url.QueryEscape(legacyHash), nil)
	rr := httptest.NewRecorder()

	s := newTestServer(t)
	s.handleRecipes(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}
	location := rr.Header().Get("Location")
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse redirect location %q: %v", location, err)
	}
	if got := u.Query().Get("h"); got != hash {
		t.Fatalf("expected redirect hash %q, got %q", hash, got)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("expected cache control header on recipes page, got %q", got)
	}
}

func TestHandleRecipes_RedirectsLegacyHashAndPreservesQuery(t *testing.T) {
	p := DefaultParams(&locations.Location{ID: "70000456", Name: "Test"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))
	hash := p.Hash()
	legacyHash, ok := legacyRecipeHash(hash)
	if !ok {
		t.Fatal("expected to derive legacy recipe hash")
	}

	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+url.QueryEscape(legacyHash)+"&mail=true&start=2026-01-25T00%3A00%3A00Z", nil)
	rr := httptest.NewRecorder()

	s := newTestServer(t)
	s.handleRecipes(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}
	location := rr.Header().Get("Location")
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse redirect location %q: %v", location, err)
	}
	if got := u.Query().Get("h"); got != hash {
		t.Fatalf("expected redirect hash %q, got %q", hash, got)
	}
}

func TestHandleRecipes_UsesSelectionForSavedAndDismissedRenderState(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	originHash := p.Hash()
	require.NoError(t, s.SaveParams(t.Context(), p))

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved"}
	dismissedRecipe := ai.Recipe{Title: "Dismissed Recipe", Description: "Dismissed"}
	saveRecipesForOrigin(t, s, originHash, savedRecipe, dismissedRecipe)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{savedRecipe, dismissedRecipe},
	}, originHash))

	require.NoError(t, s.saveRecipeSelection(t.Context(), "mock-clerk-user-id", originHash, recipeSelection{
		SavedHashes:     []string{savedRecipe.ComputeHash()},
		DismissedHashes: []string{dismissedRecipe.ComputeHash()},
	}))

	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+url.QueryEscape(originHash), nil)
	rr := httptest.NewRecorder()

	s.handleRecipes(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	require.Contains(t, body, `✓ Added`)
	require.Contains(t, body, `Restore`)
	require.Contains(t, body, `/recipes/`+originHash+`/finalize`)
	require.NotContains(t, body, `Add at least one recipe`)
}

func TestHandleRecipes_GuestSeesSaveButtonButNotHideButton(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	p := DefaultParams(&locations.Location{ID: "70004002", Name: "Store"}, time.Now())
	originHash := p.Hash()
	require.NoError(t, s.SaveParams(t.Context(), p))
	recipe := ai.Recipe{Title: "Guest Recipe", Description: "Visible save action"}
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{recipe},
	}, originHash))

	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+url.QueryEscape(originHash), nil)
	rr := httptest.NewRecorder()

	s.handleRecipes(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var guestCookie *http.Cookie
	for _, cookie := range rr.Result().Cookies() {
		if cookie.Name == guest.ShoppingListCookieName {
			guestCookie = cookie
			break
		}
	}
	require.NotNil(t, guestCookie)
	require.Equal(t, "0", guestCookie.Value)
	body := rr.Body.String()
	require.Contains(t, body, `hx-post="/recipe/`+recipe.ComputeHash()+`/save"`)
	require.Contains(t, body, `Add`)
	require.NotContains(t, body, `Hide`)
}

func TestHandleRecipes_UsesStoredUserDirectiveInSavedParamsAndHash(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	location := &locations.Location{
		ID:      "70001001",
		Name:    "Test Store",
		ZipCode: "94105",
	}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestLocationServer(staticLocationLookup{location: location}),
	)
	t.Cleanup(s.Wait)

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=70001001&date=2026-03-06&instructions=make+it+vegetarian", nil)
	currentUser, err := storage.FromRequest(t.Context(), req, auth.DefaultMock())
	if err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	currentUser.Directive = "No shellfish. Prefer high-protein dinners."
	if err := storage.Update(currentUser); err != nil {
		t.Fatalf("failed to save user directive: %v", err)
	}

	expectedParams, err := ParseQueryArgs(t.Context(), req, staticLocationLookup{location: location})
	if err != nil {
		t.Fatalf("failed to build expected params: %v", err)
	}
	baselineHash := expectedParams.Hash()
	expectedParams.Directive = currentUser.Directive
	expectedHash := expectedParams.Hash()
	if expectedHash == baselineHash {
		t.Fatal("expected stored directive to change params hash")
	}

	rr := httptest.NewRecorder()
	s.handleRecipes(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}

	locationHeader := rr.Header().Get("Location")
	if locationHeader == "" {
		t.Fatal("expected redirect location")
	}
	redirectURL, err := url.Parse(locationHeader)
	if err != nil {
		t.Fatalf("failed to parse redirect location %q: %v", locationHeader, err)
	}
	if got := redirectURL.Query().Get("h"); got != expectedHash {
		t.Fatalf("expected redirect hash %q, got %q", expectedHash, got)
	}
	if got := redirectURL.Query().Get("h"); got == baselineHash {
		t.Fatalf("expected redirect hash not to use empty-directive hash %q", baselineHash)
	}

	savedParams, err := s.ParamsFromCache(t.Context(), expectedHash)
	if err != nil {
		t.Fatalf("failed to load saved params: %v", err)
	}
	if got, want := savedParams.Directive, currentUser.Directive; got != want {
		t.Fatalf("expected saved directive %q, got %q", want, got)
	}
	if got, want := savedParams.Hash(), expectedHash; got != want {
		t.Fatalf("expected saved params hash %q, got %q", want, got)
	}
}

func TestHandleRecipes_SetsEmptyFavoriteStoreFromGeneratedLocation(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	location := &locations.Location{
		ID:      "wholefoods_70001002",
		Name:    "Test Store",
		ZipCode: "94105",
	}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestLocationServer(staticLocationLookup{location: location}),
	)
	t.Cleanup(s.Wait)

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=wholefoods_70001002&date=2026-03-06", nil)
	currentUser, err := storage.FromRequest(t.Context(), req, auth.DefaultMock())
	require.NoError(t, err)
	require.Empty(t, currentUser.FavoriteStore)

	rr := httptest.NewRecorder()
	s.handleRecipes(rr, req)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	updated, err := storage.GetByID(currentUser.ID)
	require.NoError(t, err)
	require.Equal(t, "wholefoods_70001002", updated.FavoriteStore)
	require.False(t, updated.MailOptIn)
}

func TestHandleRecipes_DoesNotOverwriteExistingFavoriteStore(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	location := &locations.Location{
		ID:      "70001003",
		Name:    "Test Store",
		ZipCode: "94105",
	}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestLocationServer(staticLocationLookup{location: location}),
	)
	t.Cleanup(s.Wait)

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=70001003&date=2026-03-06", nil)
	currentUser, err := storage.FromRequest(t.Context(), req, auth.DefaultMock())
	require.NoError(t, err)
	currentUser.FavoriteStore = "70009999"
	require.NoError(t, storage.Update(currentUser))

	rr := httptest.NewRecorder()
	s.handleRecipes(rr, req)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	updated, err := storage.GetByID(currentUser.ID)
	require.NoError(t, err)
	require.Equal(t, "70009999", updated.FavoriteStore)
}

func TestHandleRecipes_GuestCanGenerateWhenUnderCookieLimit(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestClerk(noSessionAuth{}),
		withTestGenerator(generator),
		withTestLocationServer(staticLocationLookup{location: &locations.Location{
			ID:      "70001001",
			Name:    "Test Store",
			ZipCode: "94105",
		}}),
	)
	t.Cleanup(s.Wait)

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=70001001&date=2026-03-06&instructions=make+it+vegetarian", nil)
	req.AddCookie(&http.Cookie{Name: guest.ShoppingListCookieName, Value: "1"})
	rr := httptest.NewRecorder()

	s.handleRecipes(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}
	location := rr.Header().Get("Location")
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse redirect location %q: %v", location, err)
	}
	if u.Path != "/recipes" || u.Query().Get("h") == "" || !u.Query().Has("start") {
		t.Fatalf("expected redirect to started recipe generation, got %q", location)
	}
	cookies := rr.Result().Cookies()
	var guestCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == guest.ShoppingListCookieName {
			guestCookie = cookie
			break
		}
	}
	if guestCookie == nil || guestCookie.Value != "2" {
		t.Fatalf("expected guest shopping list cookie value 2, got %#v", guestCookie)
	}
	select {
	case <-generator.called:
	case <-time.After(time.Second):
		t.Fatal("expected guest generation to start")
	}
	captured := generator.LastParams()
	if captured == nil {
		t.Fatal("expected captured generation params")
	}
	if len(captured.LastRecipes) != 0 {
		t.Fatalf("expected guest generation without last recipes, got %#v", captured.LastRecipes)
	}
}

func TestHandleRecipes_GuestRedirectsToSignInWhenGuestShoppingListCookieMissing(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestClerk(noSessionAuth{}),
		withTestGenerator(generator),
		withTestLocationServer(staticLocationLookup{location: &locations.Location{
			ID:      "70001001",
			Name:    "Test Store",
			ZipCode: "94105",
		}}),
	)

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=70001001&date=2026-03-06&instructions=make+it+vegetarian", nil)
	req.AddCookie(&http.Cookie{Name: "some_other_cookie", Value: "present"})
	rr := httptest.NewRecorder()

	s.handleRecipes(rr, req)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	require.Equal(t, signInPath("/recipes?location=70001001&date=2026-03-06&instructions=make+it+vegetarian"), rr.Header().Get("Location"))
	select {
	case <-generator.called:
		t.Fatal("expected guest generation without guest shopping list cookie not to start")
	default:
	}
	if _, err := s.ParamsFromCache(t.Context(), DefaultParams(&locations.Location{ID: "70001001", Name: "Test Store", ZipCode: "94105"}, time.Date(2026, 3, 6, 0, 0, 0, 0, time.FixedZone("PST", -8*60*60))).Hash()); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expected params not to be saved, got %v", err)
	}
}

func TestHandleRecipes_GuestRedirectsToSignInWhenCookieInvalid(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestClerk(noSessionAuth{}),
		withTestGenerator(generator),
		withTestLocationServer(staticLocationLookup{location: &locations.Location{
			ID:      "70001001",
			Name:    "Test Store",
			ZipCode: "94105",
		}}),
	)
	t.Cleanup(s.Wait)

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=70001001&instructions=make+it+vegetarian", nil)
	req.AddCookie(&http.Cookie{Name: guest.ShoppingListCookieName, Value: "wat"})
	rr := httptest.NewRecorder()

	s.handleRecipes(rr, req)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	require.Equal(t, signInPath("/recipes?location=70001001&instructions=make+it+vegetarian"), rr.Header().Get("Location"))
	select {
	case <-generator.called:
		t.Fatal("expected invalid guest cookie not to start generation")
	default:
	}
}

func TestHandleRecipes_GuestRedirectsToSignInWhenCookieLimitReached(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestClerk(noSessionAuth{}),
		withTestLocationServer(staticLocationLookup{location: &locations.Location{
			ID:      "70001001",
			Name:    "Test Store",
			ZipCode: "94105",
		}}),
	)

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=70001001&instructions=make+it+vegetarian", nil)
	req.AddCookie(&http.Cookie{Name: guest.ShoppingListCookieName, Value: "2"})
	rr := httptest.NewRecorder()

	s.handleRecipes(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}
	if got, want := rr.Header().Get("Location"), signInPath("/recipes?location=70001001&instructions=make+it+vegetarian"); got != want {
		t.Fatalf("expected redirect location %q, got %q", want, got)
	}
}

func TestHandleRecipes_GuestRedirectsToCachedHashWhenCacheHits(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestClerk(noSessionAuth{}),
		withTestLocationServer(staticLocationLookup{location: &locations.Location{
			ID:      "70001001",
			Name:    "Test Store",
			ZipCode: "94105",
		}}),
	)

	p := DefaultParams(&locations.Location{ID: "70001001", Name: "Test Store", ZipCode: "94105"}, time.Date(2026, 3, 6, 0, 0, 0, 0, time.FixedZone("PST", -8*60*60)))
	p.Instructions = "make it vegetarian"
	hash := p.Hash()
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{{Title: "Cached Recipe", Description: "Already made"}},
	}, hash); err != nil {
		t.Fatalf("failed to seed shopping list: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=70001001&date=2026-03-06&instructions=make+it+vegetarian", nil)
	rr := httptest.NewRecorder()

	s.handleRecipes(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}
	location := rr.Header().Get("Location")
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse redirect location %q: %v", location, err)
	}
	if got := u.Query().Get("h"); got != hash {
		t.Fatalf("expected redirect hash %q, got %q", hash, got)
	}
	if u.Query().Has("start") {
		t.Fatalf("expected guest cache hit redirect without start param, got %q", location)
	}
}

func TestHandleRecipes_SameRequestDifferentDirectivesProduceDifferentHashes(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	location := &locations.Location{
		ID:      "70001001",
		Name:    "Test Store",
		ZipCode: "94105",
	}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestLocationServer(staticLocationLookup{location: location}),
	)
	t.Cleanup(s.Wait)

	req := httptest.NewRequest(http.MethodGet, "/recipes?location=70001001&date=2026-03-06&instructions=make+it+vegetarian", nil)
	currentUser, err := storage.FromRequest(t.Context(), req, auth.DefaultMock())
	if err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	runRequest := func(t *testing.T, directive string) string {
		t.Helper()

		currentUser.Directive = directive
		if err := storage.Update(currentUser); err != nil {
			t.Fatalf("failed to save user directive %q: %v", directive, err)
		}

		rr := httptest.NewRecorder()
		s.handleRecipes(rr, req.Clone(t.Context()))

		if rr.Code != http.StatusSeeOther {
			t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
		}

		locationHeader := rr.Header().Get("Location")
		if locationHeader == "" {
			t.Fatal("expected redirect location")
		}
		redirectURL, err := url.Parse(locationHeader)
		if err != nil {
			t.Fatalf("failed to parse redirect location %q: %v", locationHeader, err)
		}
		hash := redirectURL.Query().Get("h")
		if hash == "" {
			t.Fatalf("expected redirect hash in %q", locationHeader)
		}

		savedParams, err := s.ParamsFromCache(t.Context(), hash)
		if err != nil {
			t.Fatalf("failed to load saved params for hash %q: %v", hash, err)
		}
		if got := savedParams.Directive; got != directive {
			t.Fatalf("expected saved directive %q, got %q", directive, got)
		}

		return hash
	}

	hash1 := runRequest(t, "No shellfish. Prefer high-protein dinners.")
	hash2 := runRequest(t, "Vegetarian meals only. Avoid mushrooms.")

	if hash1 == hash2 {
		t.Fatalf("expected different hashes for different directives, got %q", hash1)
	}
}

func TestHandleSingle_NormalizesLegacyOriginHashToCanonicalHash(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	p := DefaultParams(
		&locations.Location{ID: "70002001", Name: "Canonical Test Store"},
		time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC),
	)
	canonicalHash := p.Hash()
	legacyHash, ok := legacyRecipeHash(canonicalHash)
	if !ok {
		t.Fatal("expected to derive legacy recipe hash")
	}

	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save canonical params: %v", err)
	}

	recipe := ai.Recipe{
		Title:        "Sheet Pan Salmon",
		Description:  "Simple weeknight salmon dinner.",
		Ingredients:  []ai.Ingredient{{Name: "salmon", Quantity: "1 lb", Price: "$12"}},
		Instructions: []string{"Roast salmon and vegetables until done."},
		Health:       "High protein",
		DrinkPairing: "Pinot Noir",
		OriginHash:   legacyHash,
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, legacyHash, recipe)

	req := httptest.NewRequest(http.MethodGet, "/recipe/"+recipeHash, nil)
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSingle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "/recipes?h="+canonicalHash) {
		t.Fatalf("expected recipe page to link to canonical hash %q; body: %s", canonicalHash, body)
	}
	if strings.Contains(body, "/recipes?h="+legacyHash) {
		t.Fatalf("expected recipe page not to link to legacy hash %q; body: %s", legacyHash, body)
	}
	if !strings.Contains(body, "Canonical Test Store") {
		t.Fatalf("expected canonical params location to render, body: %s", body)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("expected cache control header on recipe page, got %q", got)
	}
}

func TestHandleSingle_LegacyOriginHashFailWhenParamsMissing(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	p := DefaultParams(
		&locations.Location{ID: "70002002", Name: "Ignored"},
		time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC),
	)
	canonicalHash := p.Hash()
	legacyHash, ok := legacyRecipeHash(canonicalHash)
	if !ok {
		t.Fatal("expected to derive legacy recipe hash")
	}

	recipe := ai.Recipe{
		Title:        "Legacy Hash Recipe",
		Description:  "Recipe with legacy origin hash and no params record.",
		Ingredients:  []ai.Ingredient{{Name: "chicken", Quantity: "1 lb", Price: "$8"}},
		Instructions: []string{"Cook chicken until done."},
		Health:       "Protein rich",
		DrinkPairing: "Sparkling water",
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, legacyHash, recipe)

	req := httptest.NewRequest(http.MethodGet, "/recipe/"+recipeHash, nil)
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSingle(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}
}

func TestHandleSingle_IncludesCachedWineRecommendation(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	p := DefaultParams(
		&locations.Location{ID: "70003001", Name: "Wine Store"},
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	)
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	recipe := ai.Recipe{
		OriginHash:   originHash,
		ResponseID:   "resp-wine-single",
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		Ingredients:  []ai.Ingredient{{Name: "chicken", Quantity: "1", Price: "$12"}},
		Instructions: []string{"Roast until done."},
		Health:       "High protein",
		DrinkPairing: "Pinot noir",
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	if err := s.SaveWine(t.Context(), recipeHash, &ai.WineSelection{
		Wines: []ai.Ingredient{
			{Name: "Light Pinot Noir", Price: "$13.99"},
		},
		Commentary: "Balances the rich chicken skin.",
	}); err != nil {
		t.Fatalf("failed to save wine recommendation: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/recipe/"+recipeHash, nil)
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSingle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Light Pinot Noir") || !strings.Contains(body, "$13.99") {
		t.Fatalf("expected cached wine picks in response, got body: %s", body)
	}
	if !strings.Contains(body, "Balances the rich chicken skin.") {
		t.Fatalf("expected cached wine commentary in response, got body: %s", body)
	}
	if strings.Contains(body, "Choose a wine") {
		t.Fatalf("expected cached recommendation to replace the wine picker, got body: %s", body)
	}
}

func TestHandleSingle_UsesUserProfileForSavedState(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	p := DefaultParams(
		&locations.Location{ID: "70003002", Name: "Single Store"},
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	)
	originHash := p.Hash()
	require.NoError(t, s.SaveParams(t.Context(), p))

	recipe := ai.Recipe{
		OriginHash:   originHash,
		Title:        "Saved Single Recipe",
		Description:  "Saved from the list page.",
		Ingredients:  []ai.Ingredient{{Name: "chicken", Quantity: "1", Price: "$12"}},
		Instructions: []string{"Roast until done."},
		Health:       "High protein",
		DrinkPairing: "Pinot noir",
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	require.NoError(t, s.storage.Update(&utypes.User{
		ID:          "mock-clerk-user-id",
		Email:       []string{"you@careme.cooking"},
		CreatedAt:   time.Now(),
		ShoppingDay: time.Saturday.String(),
		LastRecipes: []utypes.Recipe{{
			Title:     recipe.Title,
			Hash:      recipeHash,
			CreatedAt: time.Now(),
		}},
	}))

	req := httptest.NewRequest(http.MethodGet, "/recipe/"+recipeHash, nil)
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSingle(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	require.Contains(t, body, `Dismiss`)
	require.NotContains(t, body, `>Save</button>`)
}

func TestHandleSingle_GuestSeesSaveButton(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	p := DefaultParams(
		&locations.Location{ID: "70003003", Name: "Single Store"},
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	)
	originHash := p.Hash()
	require.NoError(t, s.SaveParams(t.Context(), p))

	recipe := ai.Recipe{
		OriginHash:   originHash,
		Title:        "Guest Single Recipe",
		Description:  "Guests can see save.",
		Ingredients:  []ai.Ingredient{{Name: "beans", Quantity: "1 can"}},
		Instructions: []string{"Warm gently."},
		Health:       "Fiber rich",
		DrinkPairing: "Sparkling water",
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)

	req := httptest.NewRequest(http.MethodGet, "/recipe/"+recipeHash, nil)
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSingle(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	require.Contains(t, body, `hx-post="/recipe/`+recipeHash+`/save"`)
	require.Contains(t, body, `Save`)
	require.NotContains(t, body, `Dismiss`)
}

type noSessionAuth struct{}

func (n noSessionAuth) GetUserEmail(ctx context.Context, clerkUserID string) (string, error) {
	return "", nil
}

func (n noSessionAuth) GetUserIDFromRequest(r *http.Request) (string, error) {
	return "", auth.ErrNoSession
}

func (n noSessionAuth) WithAuthHTTP(handler http.Handler) http.Handler {
	return handler
}

func (n noSessionAuth) Register(mux routing.Registrar) {}

func TestHandleQuestion_RequiresSignedInUser(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	form := url.Values{
		"response_id": {"resp-test"},
		"question":    {"Can I swap the protein?"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/question", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleQuestion(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestHandleQuestion_RejectsNonHTMXRequest(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	form := url.Values{
		"response_id": {"resp-test"},
		"question":    {"Can I swap the protein?"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/question", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleQuestion(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestHandleRegenerateSingleRecipe_ReplacesSavedRecipeWithoutChangingShoppingList(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	generator := &captureQuestionGenerator{}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestGenerator(generator),
	)

	now := time.Now()
	params := DefaultParams(&locations.Location{ID: "70001001", Name: "Store"}, now)
	shoppingListHash := params.Hash()
	original := ai.Recipe{
		Title:        "Original Steak Dinner",
		Description:  "Original.",
		Ingredients:  []ai.Ingredient{{Name: "Steak", Quantity: "1 lb"}},
		Instructions: []string{"Cook steak.", "Serve."},
		OriginHash:   shoppingListHash,
		ResponseID:   "resp-original",
	}
	originalHash := original.ComputeHash()
	params.Saved = []ai.Recipe{original}
	require.NoError(t, s.SaveParams(t.Context(), params))
	require.NoError(t, s.SaveRecipe(t.Context(), original))
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{original}}, shoppingListHash))
	require.NoError(t, s.SaveThread(t.Context(), originalHash, []RecipeThreadEntry{{
		Question:   "Can I use skirt steak?",
		Answer:     "Yes.",
		ResponseID: "resp-question",
		CreatedAt:  now,
	}}))
	user := &utypes.User{
		ID:          "mock-clerk-user-id",
		Email:       []string{"you@careme.cooking"},
		CreatedAt:   now,
		ShoppingDay: time.Saturday.String(),
		LastRecipes: []utypes.Recipe{{Title: original.Title, Hash: originalHash, CreatedAt: now}},
	}
	require.NoError(t, storage.Update(user))

	req := httptest.NewRequest(http.MethodPost, "/recipe/"+url.PathEscape(originalHash)+"/regenerate", nil)
	req.SetPathValue("hash", originalHash)
	rr := httptest.NewRecorder()

	s.handleRegenerateSingleRecipe(rr, req)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	newLocation := rr.Header().Get("Location")
	require.Contains(t, newLocation, "/recipe/")
	newHash := strings.TrimPrefix(newLocation, "/recipe/")
	require.NotEqual(t, originalHash, newHash)

	updatedUser, err := storage.GetByID("mock-clerk-user-id")
	require.NoError(t, err)
	require.Len(t, updatedUser.LastRecipes, 1)
	assert.Equal(t, newHash, updatedUser.LastRecipes[0].Hash)
	assert.Equal(t, "Updated Skirt Steak Dinner", updatedUser.LastRecipes[0].Title)

	updatedRecipe, err := s.SingleFromCache(t.Context(), newHash)
	require.NoError(t, err)
	assert.Equal(t, originalHash, updatedRecipe.ParentHash)
	assert.Equal(t, shoppingListHash, updatedRecipe.OriginHash)

	shoppingList, err := s.FromCache(t.Context(), shoppingListHash)
	require.NoError(t, err)
	require.Len(t, shoppingList.Recipes, 1)
	assert.Equal(t, originalHash, shoppingList.Recipes[0].ComputeHash())
}

type captureKickgenerationGenerator struct {
	mu     sync.Mutex
	last   *generatorParams
	err    error
	called chan struct{}
}

func (c *captureKickgenerationGenerator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	c.mu.Lock()
	clone := *p
	clone.LastRecipes = append([]string(nil), p.LastRecipes...)
	clone.PriorSavedHashes = append([]string(nil), p.PriorSavedHashes...)
	clone.PreviousMenuPlanResponseID = p.PreviousMenuPlanResponseID
	clone.Saved = append([]ai.Recipe(nil), p.Saved...)
	clone.Dismissed = append([]ai.Recipe(nil), p.Dismissed...)
	c.last = &clone
	c.mu.Unlock()
	if c.called != nil {
		select {
		case c.called <- struct{}{}:
		default:
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	return &ai.ShoppingList{}, nil
}

func (c *captureKickgenerationGenerator) RegenerateRecipe(ctx context.Context, instructions []string, previousResponseID string) (*ai.Recipe, error) {
	panic("unexpected call to RegenerateRecipe")
}

func (c *captureKickgenerationGenerator) AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error) {
	panic("unexpected call to AskQuestion")
}

func (c *captureKickgenerationGenerator) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected call to GenerateRecipeImage")
}

func (c *captureKickgenerationGenerator) PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error) {
	panic("unexpected call to PickAWine")
}

func (c *captureKickgenerationGenerator) Ready(ctx context.Context) error {
	return nil
}

func (c *captureKickgenerationGenerator) LastParams() *generatorParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		return nil
	}
	clone := *c.last
	clone.LastRecipes = append([]string(nil), c.last.LastRecipes...)
	clone.PriorSavedHashes = append([]string(nil), c.last.PriorSavedHashes...)
	clone.Saved = append([]ai.Recipe(nil), c.last.Saved...)
	clone.Dismissed = append([]ai.Recipe(nil), c.last.Dismissed...)
	return &clone
}

func TestKickgeneration_OnlyAvoidsRecentlyCookedRecipes(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestGenerator(generator),
	)
	t.Cleanup(s.Wait)

	now := time.Now()
	cookedRecent := utypes.Recipe{Title: "Cooked Recently", Hash: "hash-cooked-recent", CreatedAt: now.Add(-48 * time.Hour)}
	notCookedRecent := utypes.Recipe{Title: "Only Saved", Hash: "hash-saved-recent", CreatedAt: now.Add(-24 * time.Hour)}
	tooOldCooked := utypes.Recipe{Title: "Cooked Too Old", Hash: "hash-cooked-old", CreatedAt: now.Add(-15 * 24 * time.Hour)}

	if err := s.SaveFeedback(t.Context(), cookedRecent.Hash, feedback.Feedback{Cooked: true, UpdatedAt: now}); err != nil {
		t.Fatalf("failed to seed cooked feedback: %v", err)
	}
	if err := s.SaveFeedback(t.Context(), notCookedRecent.Hash, feedback.Feedback{Cooked: false, UpdatedAt: now}); err != nil {
		t.Fatalf("failed to seed uncooked feedback: %v", err)
	}
	if err := s.SaveFeedback(t.Context(), tooOldCooked.Hash, feedback.Feedback{Cooked: true, UpdatedAt: now}); err != nil {
		t.Fatalf("failed to seed old cooked feedback: %v", err)
	}

	params := DefaultParams(&locations.Location{ID: "70001001", Name: "Store"}, now)
	params.LastRecipes = s.recentCookedTitles(t.Context(), []utypes.Recipe{cookedRecent, notCookedRecent, tooOldCooked})
	s.kickgeneration(t.Context(), params)

	select {
	case <-generator.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for generator call")
	}

	captured := generator.LastParams()
	require.NotNil(t, captured)
	if got, want := captured.LastRecipes, []string{"Cooked Recently"}; !slices.Equal(got, want) {
		t.Fatalf("expected only recently cooked recipes in avoid list, got %v", got)
	}
}

func TestKickgeneration_WritesGeneratorErrorsToStatus(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	generator := &captureKickgenerationGenerator{err: errors.New("plan exploded")}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(generator),
	)

	params := DefaultParams(&locations.Location{ID: "70001001", Name: "Store"}, time.Now())
	s.kickgeneration(t.Context(), params)
	s.Wait()

	got, err := s.statusReader.GenerationStatusFromCache(t.Context(), params.Hash())
	require.NoError(t, err)
	assert.Equal(t, "Something went wrong: plan exploded", got)
}

func TestKickgeneration_HidesStaplesErrorsFromStatus(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	cause := errors.New(`ingredient grade returned unknown product_id "0000000004130"`)
	generator := &captureKickgenerationGenerator{err: fmt.Errorf("failed to get staples: %w", &staplesUnavailableError{
		attempts: 4,
		err:      cause,
	})}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(generator),
	)

	params := DefaultParams(&locations.Location{ID: "wholefoods_10260", Name: "Whole Foods"}, time.Now())
	s.kickgeneration(t.Context(), params)
	s.Wait()

	got, err := s.statusReader.GenerationStatusFromCache(t.Context(), params.Hash())
	require.NoError(t, err)
	assert.Equal(t, recipestatus.StaplesUnavailable(), got)
	assert.NotContains(t, got, "product_id")
	assert.NotContains(t, got, "0000000004130")
}

func TestKickGenerationIfNotPresent_DoesNotKickExistingParams(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(generator),
	)

	params := DefaultParams(&locations.Location{ID: "70001001", Name: "Store"}, time.Now())
	require.NoError(t, s.SaveParams(t.Context(), params))

	err := s.KickGenerationIfNotPresent(t.Context(), params)
	require.NoError(t, err)

	select {
	case <-generator.called:
		t.Fatal("unexpected generator call")
	default:
	}
}

func TestKickGenerationIfNotPresent_SavesParamsAndKicksMissingShoppingList(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(generator),
	)
	t.Cleanup(s.Wait)

	params := DefaultParams(&locations.Location{ID: "70001001", Name: "Store"}, time.Now())
	err := s.KickGenerationIfNotPresent(t.Context(), params)
	require.NoError(t, err)

	select {
	case <-generator.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for generator call")
	}

	_, err = s.ParamsFromCache(t.Context(), params.Hash())
	require.NoError(t, err)
}

func TestSpin_RendersCachedGenerationStatus(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	hash := "spinner-hash"
	status := "Baby we working"
	writer := s.statusReader.(*statusStore)
	err := writer.SaveGenerationStatus(t.Context(), hash, status)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+hash+"&start=2026-07-10T00:00:00Z", nil)

	s.spin(t.Context(), rr, req, hash)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	assert.Contains(t, rr.Body.String(), status)
	assert.Contains(t, rr.Body.String(), `hx-get="/recipes?h=`+hash+`&amp;start=2026-07-10T00:00:00Z"`)
	assert.NotContains(t, rr.Body.String(), `http-equiv="refresh"`)
}

func TestSpin_HTMXRequestRendersProgressFragment(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	hash := "spinner-hash"
	status := "Still chopping"
	writer := s.statusReader.(*statusStore)
	err := writer.SaveGenerationStatus(t.Context(), hash, status)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+hash+"&start=2026-07-10T00:00:00Z", nil)
	req.Header.Set("HX-Request", "true")

	s.spin(t.Context(), rr, req, hash)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, `id="spin-page-work"`)
	assert.Contains(t, body, status)
	assert.Contains(t, body, `hx-trigger="load delay:10s"`)
	assert.NotContains(t, body, "<!doctype html>")
}

type captureQuestionGenerator struct {
	lastQuestion   string
	lastResponseID string
	lastWinePick   struct {
		recipeTitle string
		date        time.Time
	}
	wineRecommendation string
	winePickCalls      int
	panicOnWine        bool
}

func (c *captureQuestionGenerator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	return &ai.ShoppingList{}, nil
}

func (c *captureQuestionGenerator) RegenerateRecipe(ctx context.Context, instructions []string, previousResponseID string) (*ai.Recipe, error) {
	return &ai.Recipe{
		Title:        "Updated Skirt Steak Dinner",
		Description:  "Updated after questions.",
		Ingredients:  []ai.Ingredient{{Name: "Skirt steak", Quantity: "1 lb"}},
		Instructions: []string{"Cook the steak.", "Serve."},
		ResponseID:   "resp-regenerated",
	}, nil
}

func (c *captureQuestionGenerator) AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error) {
	c.lastQuestion = question
	c.lastResponseID = previousResponseID
	return &ai.QuestionResponse{
		Answer:     "Try chicken thighs at the same cook time.",
		ResponseID: "resp-next",
	}, nil
}

func (c *captureQuestionGenerator) PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error) {
	if c.panicOnWine {
		panic("unexpected call to PickAWine")
	}
	_ = location
	c.winePickCalls++
	c.lastWinePick.recipeTitle = recipe.Title
	c.lastWinePick.date = date
	if c.wineRecommendation != "" {
		return &ai.WineSelection{Commentary: c.wineRecommendation, Wines: []ai.Ingredient{}}, nil
	}
	return &ai.WineSelection{Commentary: "Try a chilled sauvignon blanc.", Wines: []ai.Ingredient{}}, nil
}

func (c *captureQuestionGenerator) Ready(ctx context.Context) error {
	return nil
}

type countingImageGenerator struct {
	imageCalls   int
	panicOnImage bool
	imageBody    []byte
}

func (c *countingImageGenerator) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	if c.panicOnImage {
		panic("unexpected call to GenerateRecipeImage")
	}
	_ = ctx
	_ = recipe
	c.imageCalls++
	body := c.imageBody
	if len(body) == 0 {
		body = []byte("webp-bytes")
	}
	return &ai.GeneratedImage{
		Body: bytes.NewReader(body),
	}, nil
}

func seedQuestionConversation(t *testing.T, s *server, responseID string) string {
	t.Helper()

	p := DefaultParams(&locations.Location{ID: "70003002", Name: "Question Test Store"}, time.Now())
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipe := ai.Recipe{
		OriginHash:   originHash,
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		Ingredients:  []ai.Ingredient{{Name: "chicken", Quantity: "1", Price: "$12"}},
		Instructions: []string{"Roast until done."},
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{recipe},
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}
	return recipeHash
}

func TestHandleQuestion_HTMXReturnsThreadFragment(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(&captureQuestionGenerator{}),
	)

	recipeHash := seedQuestionConversation(t, s, "resp-test")

	form := url.Values{
		"response_id": {"resp-test"},
		"question":    {"Can I swap the protein?"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/question", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleQuestion(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "" {
		t.Fatalf("expected no redirect location for HTMX request, got %q", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `id="question-thread"`) {
		t.Fatalf("expected thread fragment in response, got body: %s", body)
	}
	if !strings.Contains(body, "Can I swap the protein?") {
		t.Fatalf("expected question in response, got body: %s", body)
	}
	if !strings.Contains(body, "Try chicken thighs at the same cook time.") {
		t.Fatalf("expected answer in response, got body: %s", body)
	}
	if got, want := s.generator.(*captureQuestionGenerator).lastResponseID, "resp-test"; got != want {
		t.Fatalf("expected generator response ID %q, got %q", want, got)
	}
	if !strings.Contains(body, `name="response_id" value="resp-next"`) {
		t.Fatalf("expected updated response id in thread fragment, got body: %s", body)
	}
	if !strings.Contains(body, `action="/recipe/`+recipeHash+`/regenerate"`) || !strings.Contains(body, "Tweak it, chef") {
		t.Fatalf("expected regenerate action after first question, got body: %s", body)
	}
	if !strings.Contains(body, `button.textContent='Tweaking...'; button.disabled=true;`) {
		t.Fatalf("expected regenerate action to show its pending state, got body: %s", body)
	}
}

func TestHandleQuestion_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	form := url.Values{
		"response_id": {"resp-test"},
		"question":    {"Can I swap the protein?"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/question", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleQuestion(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got, want := rr.Header().Get("HX-Redirect"), signInPath("/recipe/hash/question"); got != want {
		t.Fatalf("expected HX-Redirect %q, got %q", want, got)
	}
}

func TestHandleQuestion_PrependsRecipeTitleForModelQuestion(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	g := &captureQuestionGenerator{}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(g),
	)

	recipeHash := seedQuestionConversation(t, s, "resp-test")

	form := url.Values{
		"response_id":  {"resp-test"},
		"question":     {"Can I swap the protein?"},
		"recipe_title": {"BBQ Pulled Pork"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/question", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleQuestion(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if got, want := g.lastQuestion, "Regarding BBQ Pulled Pork: Can I swap the protein?"; got != want {
		t.Fatalf("expected generator question %q, got %q", want, got)
	}
}

func TestHandleRecipeImage_ServesCachedImageWithoutGenerator(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	g := &countingImageGenerator{panicOnImage: true}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withImageGenerator(g),
	)

	recipe := ai.Recipe{
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		Ingredients:  []ai.Ingredient{{Name: "chicken", Quantity: "1", Price: "$12"}},
		Instructions: []string{"Roast until done."},
	}
	recipeHash := recipe.ComputeHash()
	imageBody := []byte{'R', 'I', 'F', 'F', 0x24, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '}
	if err := s.SaveRecipeImage(t.Context(), recipeHash, &ai.GeneratedImage{Body: bytes.NewReader(imageBody)}); err != nil {
		t.Fatalf("failed to seed recipe image: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/recipe/"+recipeHash+"/image", nil)
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleRecipeImage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
	if got, want := rr.Header().Get("Content-Type"), http.DetectContentType(imageBody); got != want {
		t.Fatalf("expected %q content type, got %q", want, got)
	}
	if got := rr.Body.Bytes(); !bytes.Equal(got, imageBody) {
		t.Fatalf("expected cached image bytes %v, got %v", imageBody, got)
	}
	if got, want := g.imageCalls, 0; got != want {
		t.Fatalf("expected GenerateRecipeImage call count %d, got %d", want, got)
	}
}

func TestHandleSaveRecipe_SavesRecipeToUserProfile(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)

	recipe := ai.Recipe{
		Title:       "Save Me",
		Description: "Recipe to save",
		ResponseID:  "resp-123",
	}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{recipe},
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	form := url.Values{"h": {originHash}}
	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)
	s.Wait()

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	require.Equal(t, "careme:saved-recipes-changed", rr.Header().Get("HX-Trigger"))
	require.Contains(t, rr.Body.String(), `id="shopping-recipe-`+recipeHash+`"`)
	require.Contains(t, rr.Body.String(), `✓ Added`)
	require.Contains(t, rr.Body.String(), `Hide`)
	require.Contains(t, rr.Body.String(), `/dismiss"`)
	require.NotContains(t, rr.Body.String(), `/save"`)
	if !strings.Contains(rr.Body.String(), `id="shopping-finalize-controls"`) || !strings.Contains(rr.Body.String(), `hx-swap-oob="outerHTML"`) {
		t.Fatalf("expected finalize controls oob response, got body: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `/recipes/`+originHash+`/finalize`) {
		t.Fatalf("expected finalize button to become enabled after save, got body: %s", rr.Body.String())
	}

	user, err := storage.GetByID("mock-clerk-user-id")
	if err != nil {
		t.Fatalf("failed to load user: %v", err)
	}
	if len(user.LastRecipes) != 1 {
		t.Fatalf("expected one saved recipe, got %d", len(user.LastRecipes))
	}
	if user.LastRecipes[0].Hash != recipeHash {
		t.Fatalf("expected saved hash %q, got %q", recipeHash, user.LastRecipes[0].Hash)
	}
	selection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", originHash)
	if err != nil {
		t.Fatalf("failed to load selection: %v", err)
	}
	if len(selection.SavedHashes) != 1 || selection.SavedHashes[0] != recipeHash {
		t.Fatalf("expected saved selection with hash %q, got %#v", recipeHash, selection.SavedHashes)
	}
}

func TestHandleSaveRecipe_NoSessionHTMXSetsRedirectHeaderToShoppingListPendingSave(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	form := url.Values{"h": {"shopping-hash"}}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got, want := rr.Header().Get("HX-Redirect"), signInPath("/recipes?h=shopping-hash&save=hash"); got != want {
		t.Fatalf("expected HX-Redirect %q, got %q", want, got)
	}
}

func TestHandleSaveRecipe_NoSessionFromRecipePageRedirectsToShoppingListPendingSave(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	form := url.Values{"h": {"origin-hash"}, "source": {"recipe"}}
	req := httptest.NewRequest(http.MethodPost, "/recipe/recipe-hash/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "recipe-hash")
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Equal(t, signInPath("/recipes?h=origin-hash&save=recipe-hash"), rr.Header().Get("HX-Redirect"))
}

func TestHandleRecipes_PendingSaveAfterSignInAddsRecipeAndRedirects(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)

	recipe := ai.Recipe{
		Title:       "Save After Login",
		Description: "Recipe to save after login",
		ResponseID:  "resp-pending-save",
	}
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	hash := params.Hash()
	recipeHash := recipe.ComputeHash()
	require.NoError(t, s.SaveParams(t.Context(), params))
	saveRecipesForOrigin(t, s, hash, recipe)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{recipe}}, hash))

	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+hash+"&save="+recipeHash, nil)
	rr := httptest.NewRecorder()

	s.handleRecipes(rr, req)
	s.Wait()

	require.Equal(t, http.StatusSeeOther, rr.Code)
	require.Equal(t, "/recipes?h="+hash, rr.Header().Get("Location"))
	selection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", hash)
	require.NoError(t, err)
	require.Equal(t, []string{recipeHash}, selection.SavedHashes)
	user, err := storage.GetByID("mock-clerk-user-id")
	require.NoError(t, err)
	require.Len(t, user.LastRecipes, 1)
	require.Equal(t, recipeHash, user.LastRecipes[0].Hash)
}

func TestHandleSaveRecipe_UsesRequestHashForSelectionKey(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)

	recipe := ai.Recipe{
		Title:       "Save Me",
		Description: "Recipe to save",
		ResponseID:  "resp-123",
	}
	currentParams := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	currentHash := currentParams.Hash()
	if err := s.SaveParams(t.Context(), currentParams); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, "stale-origin-hash", recipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{recipe},
	}, currentHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/save?h="+currentHash, nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)
	s.Wait()

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	currentSelection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", currentHash)
	if err != nil {
		t.Fatalf("failed to load current hash selection: %v", err)
	}
	if len(currentSelection.SavedHashes) != 1 || currentSelection.SavedHashes[0] != recipeHash {
		t.Fatalf("expected saved selection under current hash, got %#v", currentSelection.SavedHashes)
	}
	staleSelection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", "stale-origin-hash")
	if err != nil {
		t.Fatalf("failed to load stale hash selection: %v", err)
	}
	if !staleSelection.Empty() {
		t.Fatalf("expected no selection under stale origin hash, got %#v", staleSelection)
	}
}

func TestHandleSaveRecipe_RestoresDismissedRecipeCard(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)

	recipe := ai.Recipe{
		Title:        "Recovered Recipe",
		Description:  "Recipe to recover",
		Ingredients:  []ai.Ingredient{{Name: "ingredient1", Quantity: "1 cup", Price: "2.00"}},
		Instructions: []string{"Step 1"},
		Health:       "Healthy",
		DrinkPairing: "Water",
	}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	originHash := p.Hash()
	require.NoError(t, s.SaveParams(t.Context(), p))
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{recipe}}, originHash))
	require.NoError(t, s.saveRecipeSelection(t.Context(), "mock-clerk-user-id", originHash, recipeSelection{
		DismissedHashes: []string{recipeHash},
	}))

	form := url.Values{"h": {originHash}}
	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)
	s.Wait()

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), `id="shopping-recipe-`+recipeHash+`"`)
	require.Contains(t, rr.Body.String(), `Recipe to recover`)
	require.Contains(t, rr.Body.String(), `Details`)
	require.Contains(t, rr.Body.String(), `✓ Added`)
	require.Contains(t, rr.Body.String(), `Hide`)
	require.Contains(t, rr.Body.String(), `/dismiss"`)
	require.NotContains(t, rr.Body.String(), `Set aside for this round.`)
	require.NotContains(t, rr.Body.String(), `/save"`)

	selection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", originHash)
	require.NoError(t, err)
	require.Equal(t, []string{recipeHash}, selection.SavedHashes)
	require.Empty(t, selection.DismissedHashes)
}

func TestHandleSaveRecipe_FromRecipePageReturnsSaveAction(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)

	recipe := ai.Recipe{
		Title:        "Single Recipe",
		Description:  "Recipe to save from detail page",
		Ingredients:  []ai.Ingredient{{Name: "ingredient1", Quantity: "1 cup", Price: "2.00"}},
		Instructions: []string{"Step 1"},
		Health:       "Healthy",
		DrinkPairing: "Water",
	}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	originHash := p.Hash()
	require.NoError(t, s.SaveParams(t.Context(), p))
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{recipe}}, originHash))

	form := url.Values{"h": {originHash}, "source": {"recipe"}}
	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)
	s.Wait()

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), `class="recipe-save-action pt-2"`)
	require.Contains(t, rr.Body.String(), `Dismiss`)
	require.Contains(t, rr.Body.String(), `/dismiss"`)
	require.Contains(t, rr.Body.String(), `"source":"recipe"`)
	require.NotContains(t, rr.Body.String(), `id="shopping-recipe-`+recipeHash+`"`)
	require.NotContains(t, rr.Body.String(), `Recipe to save from detail page`)
	require.NotContains(t, rr.Body.String(), `id="shopping-finalize-controls"`)
	require.NotContains(t, rr.Body.String(), `/save"`)
}

func TestHandleSaveRecipe_StartsBackgroundWineAndImageGeneration(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	g := &captureQuestionGenerator{
		wineRecommendation: "Bright enough for dinner.",
	}
	ig := &countingImageGenerator{
		imageBody: []byte("saved-image-bytes"),
	}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestGenerator(g),
		withImageGenerator(ig),
	)

	recipe := ai.Recipe{
		Title:       "Background Save",
		Description: "Recipe to save",
	}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	originHash := p.Hash()
	require.NoError(t, s.SaveParams(t.Context(), p))
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{recipe}}, originHash))

	form := url.Values{"h": {originHash}}
	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), `id="shopping-recipe-`+recipeHash+`"`)
	require.Contains(t, rr.Body.String(), `✓ Added`)
	require.Contains(t, rr.Body.String(), `/dismiss"`)

	s.Wait()
	assert.Equal(t, 1, g.winePickCalls)
	assert.Equal(t, 1, ig.imageCalls)

	wine, err := s.WineFromCache(t.Context(), recipeHash)
	require.NoError(t, err)
	require.NotNil(t, wine)
	assert.Equal(t, "Bright enough for dinner.", wine.Commentary)

	imageBody, err := s.RecipeImageFromCache(t.Context(), recipeHash)
	require.NoError(t, err)
	defer func() { require.NoError(t, imageBody.Close()) }()
	gotImage, err := io.ReadAll(imageBody)
	require.NoError(t, err)
	assert.Equal(t, []byte("saved-image-bytes"), gotImage)
}

func TestHandleDismissRecipe_RemovesRecipeFromUserProfile(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)

	recipe := ai.Recipe{
		Title:       "Dismiss Recipe",
		Description: "Recipe to dismiss",
		ResponseID:  "resp-123",
	}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	p.Saved = []ai.Recipe{recipe}
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{recipe},
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	user := &utypes.User{
		ID:          "mock-clerk-user-id",
		Email:       []string{"you@careme.cooking"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{
			{
				Title:     "Keep Recipe",
				Hash:      "keep-hash",
				CreatedAt: time.Now().Add(-1 * time.Hour),
			},
			{
				Title:     "Dismiss Recipe",
				Hash:      recipeHash,
				CreatedAt: time.Now(),
			},
		},
	}
	if err := storage.Update(user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	form := url.Values{"h": {originHash}}
	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/dismiss", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleDismissRecipe(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	require.Empty(t, rr.Header().Get("HX-Trigger"))
	require.Contains(t, rr.Body.String(), `id="shopping-recipe-`+recipeHash+`"`)
	require.Contains(t, rr.Body.String(), `/save"`)
	require.Contains(t, rr.Body.String(), `Restore`)
	require.NotContains(t, rr.Body.String(), `Dismissed`)
	require.NotContains(t, rr.Body.String(), `Hide`)
	require.NotContains(t, rr.Body.String(), `✓ Added`)
	require.NotContains(t, rr.Body.String(), `Recipe to dismiss`)
	require.NotContains(t, rr.Body.String(), `Details`)
	if !strings.Contains(rr.Body.String(), `id="shopping-finalize-controls"`) || !strings.Contains(rr.Body.String(), `hx-swap-oob="outerHTML"`) {
		t.Fatalf("expected finalize controls oob response, got body: %s", rr.Body.String())
	}

	updated, err := storage.GetByID("mock-clerk-user-id")
	if err != nil {
		t.Fatalf("failed to load user: %v", err)
	}
	if len(updated.LastRecipes) != 1 {
		t.Fatalf("expected one recipe after dismiss, got %d", len(updated.LastRecipes))
	}
	if updated.LastRecipes[0].Hash != "keep-hash" {
		t.Fatalf("expected remaining hash keep-hash, got %q", updated.LastRecipes[0].Hash)
	}
	selection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", originHash)
	if err != nil {
		t.Fatalf("failed to load selection: %v", err)
	}
	if len(selection.DismissedHashes) != 1 || selection.DismissedHashes[0] != recipeHash {
		t.Fatalf("expected dismissed selection with hash %q, got %#v", recipeHash, selection.DismissedHashes)
	}
}

func TestHandleDismissRecipe_FromRecipePageReturnsSaveAction(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)

	recipe := ai.Recipe{
		Title:       "Single Recipe",
		Description: "Recipe to dismiss from detail page",
	}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	p.Saved = []ai.Recipe{recipe}
	originHash := p.Hash()
	require.NoError(t, s.SaveParams(t.Context(), p))
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{recipe}}, originHash))

	user := &utypes.User{
		ID:          "mock-clerk-user-id",
		Email:       []string{"you@careme.cooking"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{
			{
				Title:     "Single Recipe",
				Hash:      recipeHash,
				CreatedAt: time.Now(),
			},
		},
	}
	require.NoError(t, storage.Update(user))

	form := url.Values{"h": {originHash}, "source": {"recipe"}}
	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/dismiss", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleDismissRecipe(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), `class="recipe-save-action pt-2"`)
	require.Contains(t, rr.Body.String(), `Save`)
	require.Contains(t, rr.Body.String(), `/save"`)
	require.Contains(t, rr.Body.String(), `"source":"recipe"`)
	require.NotContains(t, rr.Body.String(), `id="shopping-recipe-`+recipeHash+`"`)
	require.NotContains(t, rr.Body.String(), `Recipe to dismiss from detail page`)
	require.NotContains(t, rr.Body.String(), `id="shopping-finalize-controls"`)
	require.NotContains(t, rr.Body.String(), `/dismiss"`)
}

func TestHandleDismissRecipe_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/dismiss", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleDismissRecipe(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got, want := rr.Header().Get("HX-Redirect"), signInPath("/recipe/hash/dismiss"); got != want {
		t.Fatalf("expected HX-Redirect %q, got %q", want, got)
	}
}

func TestHandleDismissRecipe_UsesRequestHashForSelectionKey(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)

	recipe := ai.Recipe{
		Title:       "Dismiss Recipe",
		Description: "Recipe to dismiss",
		ResponseID:  "resp-123",
	}
	currentParams := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	currentHash := currentParams.Hash()
	if err := s.SaveParams(t.Context(), currentParams); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, "stale-origin-hash", recipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{recipe},
	}, currentHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	user := &utypes.User{
		ID:          "mock-clerk-user-id",
		Email:       []string{"you@careme.cooking"},
		CreatedAt:   time.Now(),
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{
			{
				Title:     "Dismiss Recipe",
				Hash:      recipeHash,
				CreatedAt: time.Now(),
			},
		},
	}
	if err := storage.Update(user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/dismiss?h="+currentHash, nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleDismissRecipe(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	currentSelection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", currentHash)
	if err != nil {
		t.Fatalf("failed to load current hash selection: %v", err)
	}
	if len(currentSelection.DismissedHashes) != 1 || currentSelection.DismissedHashes[0] != recipeHash {
		t.Fatalf("expected dismissed selection under current hash, got %#v", currentSelection.DismissedHashes)
	}
	staleSelection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", "stale-origin-hash")
	if err != nil {
		t.Fatalf("failed to load stale hash selection: %v", err)
	}
	if !staleSelection.Empty() {
		t.Fatalf("expected no selection under stale origin hash, got %#v", staleSelection)
	}
}

func TestHandleRegenerate_UsesServerSideSelectionAndRedirects(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)
	t.Cleanup(s.Wait)

	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved", ResponseID: "resp-saved"}
	dismissedRecipe := ai.Recipe{Title: "Dismissed Recipe", Description: "Dismissed", ResponseID: "resp-dismissed"}
	saveRecipesForOrigin(t, s, originHash, savedRecipe, dismissedRecipe)
	shoppingList := &ai.ShoppingList{
		Recipes: []ai.Recipe{savedRecipe, dismissedRecipe},
		Plan:    &ai.MenuPlan{ResponseID: "resp-menu-original"},
	}
	if err := s.SaveShoppingList(t.Context(), shoppingList, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	selection := recipeSelection{
		SavedHashes:     []string{savedRecipe.ComputeHash()},
		DismissedHashes: []string{dismissedRecipe.ComputeHash()},
	}
	if err := s.saveRecipeSelection(t.Context(), "mock-clerk-user-id", originHash, selection); err != nil {
		t.Fatalf("failed to save selection: %v", err)
	}

	form := url.Values{"instructions": {"make it vegetarian"}}
	req := httptest.NewRequest(http.MethodPost, "/recipes/"+originHash+"/regenerate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", originHash)
	rr := httptest.NewRecorder()

	s.handleRegenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	location := rr.Header().Get("HX-Redirect")
	if location == "" {
		t.Fatal("expected HX-Redirect header")
	}
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse HX-Redirect: %v", err)
	}
	newHash := u.Query().Get("h")
	if newHash == "" {
		t.Fatalf("expected redirect hash in %q", location)
	}
	if newHash == originHash {
		t.Fatal("expected a new hash after regenerate")
	}

	updatedParams, err := s.ParamsFromCache(t.Context(), newHash)
	if err != nil {
		t.Fatalf("failed to load new params: %v", err)
	}
	if updatedParams.Instructions != "make it vegetarian" {
		t.Fatalf("expected instructions to persist, got %q", updatedParams.Instructions)
	}
	if len(updatedParams.Saved) != 1 || updatedParams.Saved[0].ComputeHash() != savedRecipe.ComputeHash() {
		t.Fatalf("expected saved recipe selection to persist in params, got %#v", updatedParams.Saved)
	}
	if len(updatedParams.Dismissed) != 1 || updatedParams.Dismissed[0].ComputeHash() != dismissedRecipe.ComputeHash() {
		t.Fatalf("expected dismissed recipe selection to persist in params, got %#v", updatedParams.Dismissed)
	}
}

func TestHandleRegenerate_GuestUsesRemainingGenerationAndRedirects(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestClerk(noSessionAuth{}),
		withTestGenerator(generator),
	)
	t.Cleanup(s.Wait)

	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipe := ai.Recipe{Title: "Guest Recipe", Description: "Guest", ResponseID: "resp-guest"}
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{recipe},
		Plan:    &ai.MenuPlan{ResponseID: "resp-menu-original"},
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	form := url.Values{"instructions": {"make it vegetarian"}}
	req := httptest.NewRequest(http.MethodPost, "/recipes/"+originHash+"/regenerate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: guest.ShoppingListCookieName, Value: "1"})
	req.SetPathValue("hash", originHash)
	rr := httptest.NewRecorder()

	s.handleRegenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	location := rr.Header().Get("HX-Redirect")
	if location == "" {
		t.Fatal("expected HX-Redirect header")
	}
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse HX-Redirect: %v", err)
	}
	newHash := u.Query().Get("h")
	if newHash == "" || newHash == originHash {
		t.Fatalf("expected new regenerate hash, got %q", newHash)
	}
	var guestCookie *http.Cookie
	for _, cookie := range rr.Result().Cookies() {
		if cookie.Name == guest.ShoppingListCookieName {
			guestCookie = cookie
			break
		}
	}
	if guestCookie == nil || guestCookie.Value != "2" {
		t.Fatalf("expected guest generation cookie value 2, got %#v", guestCookie)
	}
	select {
	case <-generator.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for generator call")
	}
	captured := generator.LastParams()
	require.NotNil(t, captured)
	require.Equal(t, "make it vegetarian", captured.Instructions)
	require.Equal(t, "resp-menu-original", captured.PreviousMenuPlanResponseID)
	require.Empty(t, captured.Saved)
	require.Len(t, captured.Dismissed, 1)
	require.Equal(t, recipe.ComputeHash(), captured.Dismissed[0].ComputeHash())
	require.Empty(t, captured.LastRecipes)
}

func TestHandleRegenerate_GuestRedirectsToSignInWhenCookieMissing(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestClerk(noSessionAuth{}),
	)

	req := httptest.NewRequest(http.MethodPost, "/recipes/origin-hash/regenerate", nil)
	req.SetPathValue("hash", "origin-hash")
	rr := httptest.NewRecorder()

	s.handleRegenerate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}
	if got, want := rr.Header().Get("Location"), signInPath("/recipes/origin-hash/regenerate"); got != want {
		t.Fatalf("expected redirect location %q, got %q", want, got)
	}
}

func TestHandleRegenerate_GuestRedirectsToSignInWhenCookieLimitReached(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestClerk(noSessionAuth{}),
	)

	req := httptest.NewRequest(http.MethodPost, "/recipes/origin-hash/regenerate", nil)
	req.AddCookie(&http.Cookie{Name: guest.ShoppingListCookieName, Value: "2"})
	req.SetPathValue("hash", "origin-hash")
	rr := httptest.NewRecorder()

	s.handleRegenerate(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}
	if got, want := rr.Header().Get("Location"), signInPath("/recipes/origin-hash/regenerate"); got != want {
		t.Fatalf("expected redirect location %q, got %q", want, got)
	}
}

func TestHandleRegenerate_GuestHTMXRedirectsToSignInWhenCookieLimitReached(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestClerk(noSessionAuth{}),
	)

	req := httptest.NewRequest(http.MethodPost, "/recipes/origin-hash/regenerate", nil)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(&http.Cookie{Name: guest.ShoppingListCookieName, Value: "2"})
	req.SetPathValue("hash", "origin-hash")
	rr := httptest.NewRecorder()

	s.handleRegenerate(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got, want := rr.Header().Get("HX-Redirect"), signInPath("/recipes/origin-hash/regenerate"); got != want {
		t.Fatalf("expected HX-Redirect %q, got %q", want, got)
	}
}

func TestHandleRegenerate_PassesPriorSavedHashesAndDismissesUnsavedRecipesToGenerator(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestGenerator(generator),
	)
	t.Cleanup(s.Wait)

	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier", ResponseID: "resp-already"}
	newlySaved := ai.Recipe{Title: "Newly Saved", Description: "Saved now", ResponseID: "resp-newly"}
	available := ai.Recipe{Title: "Still Available", Description: "Fresh", ResponseID: "resp-available"}

	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	p.Saved = []ai.Recipe{alreadySaved}
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	saveRecipesForOrigin(t, s, originHash, alreadySaved, newlySaved, available)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{alreadySaved, newlySaved, available},
		Plan:    &ai.MenuPlan{ResponseID: "resp-menu-old"},
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	if err := s.saveRecipeSelection(t.Context(), "mock-clerk-user-id", originHash, recipeSelection{
		SavedHashes: []string{alreadySaved.ComputeHash(), newlySaved.ComputeHash()},
	}); err != nil {
		t.Fatalf("failed to save selection: %v", err)
	}

	form := url.Values{"instructions": {"make it faster"}}
	req := httptest.NewRequest(http.MethodPost, "/recipes/"+originHash+"/regenerate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", originHash)
	rr := httptest.NewRecorder()

	s.handleRegenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	select {
	case <-generator.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for generator call")
	}

	captured := generator.LastParams()
	require.NotNil(t, captured)
	if got, want := captured.PriorSavedHashes, []string{alreadySaved.ComputeHash()}; !slices.Equal(got, want) {
		t.Fatalf("expected prior saved hashes %v, got %v", want, got)
	}
	if got := captured.PreviousMenuPlanResponseID; got != "resp-menu-old" {
		t.Fatalf("expected previous menu plan response id %q, got %q", "resp-menu-old", got)
	}
	if len(captured.Saved) != 2 {
		t.Fatalf("expected both current saved recipes, got %#v", captured.Saved)
	}
	if len(captured.Dismissed) != 1 || captured.Dismissed[0].ComputeHash() != available.ComputeHash() {
		t.Fatalf("expected only unsaved current recipes to be dismissed, got %#v", captured.Dismissed)
	}
}

func TestHandleRegenerate_AllRecipesSavedDoesNotCarryBaseDismissed(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestGenerator(generator),
	)
	t.Cleanup(s.Wait)

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved", ResponseID: "resp-saved"}
	staleDismissedRecipe := ai.Recipe{Title: "Old Dismissed Recipe", Description: "Dismissed earlier", ResponseID: "resp-old"}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	p.Saved = []ai.Recipe{savedRecipe}
	p.Dismissed = []ai.Recipe{staleDismissedRecipe}
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	saveRecipesForOrigin(t, s, originHash, savedRecipe, staleDismissedRecipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{savedRecipe},
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	form := url.Values{"instructions": {"make it brighter"}}
	req := httptest.NewRequest(http.MethodPost, "/recipes/"+originHash+"/regenerate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", originHash)
	rr := httptest.NewRecorder()

	s.handleRegenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	select {
	case <-generator.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for generator call")
	}

	captured := generator.LastParams()
	require.NotNil(t, captured)
	if len(captured.Saved) != 1 || captured.Saved[0].ComputeHash() != savedRecipe.ComputeHash() {
		t.Fatalf("expected saved recipe to persist, got %#v", captured.Saved)
	}
	if len(captured.Dismissed) != 0 {
		t.Fatalf("expected stale dismissed recipe to be dropped, got %#v", captured.Dismissed)
	}
}

func TestHandleFinalize_UsesServerSideSelection(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
	)

	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved", ResponseID: "resp-saved"}
	dismissedRecipe := ai.Recipe{Title: "Dismissed Recipe", Description: "Dismissed", ResponseID: "resp-dismissed"}
	saveRecipesForOrigin(t, s, originHash, savedRecipe, dismissedRecipe)
	shoppingList := &ai.ShoppingList{
		Recipes: []ai.Recipe{savedRecipe, dismissedRecipe},
		Plan:    &ai.MenuPlan{ResponseID: "resp-menu-original"},
	}
	if err := s.SaveShoppingList(t.Context(), shoppingList, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	selection := recipeSelection{
		SavedHashes:     []string{savedRecipe.ComputeHash()},
		DismissedHashes: []string{dismissedRecipe.ComputeHash()},
	}
	if err := s.saveRecipeSelection(t.Context(), "mock-clerk-user-id", originHash, selection); err != nil {
		t.Fatalf("failed to save selection: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/recipes/"+originHash+"/finalize", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", originHash)
	rr := httptest.NewRecorder()

	s.handleFinalize(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	location := rr.Header().Get("HX-Redirect")
	if location == "" {
		t.Fatal("expected HX-Redirect header")
	}
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("failed to parse HX-Redirect: %v", err)
	}
	finalHash := u.Query().Get("h")
	if finalHash == "" {
		t.Fatalf("expected redirect hash in %q", location)
	}

	finalList, err := s.FromCache(t.Context(), finalHash)
	if err != nil {
		t.Fatalf("failed to load finalized list: %v", err)
	}
	if len(finalList.Recipes) != 1 || finalList.Recipes[0].ComputeHash() != savedRecipe.ComputeHash() {
		t.Fatalf("expected only saved recipe in finalized list, got %#v", finalList.Recipes)
	}
	if finalList.Plan == nil || finalList.Plan.ResponseID != "resp-menu-original" {
		t.Fatalf("expected finalized list to preserve menu plan response id, got %+v", finalList.Plan)
	}
}

func TestParamsForAction_PreservesBaseSavedSelectionAndDropsBaseDismissedWhenSelectionCacheEmpty(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved"}
	dismissedRecipe := ai.Recipe{Title: "Dismissed Recipe", Description: "Dismissed"}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	p.Saved = []ai.Recipe{savedRecipe}
	p.Dismissed = []ai.Recipe{dismissedRecipe}
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{savedRecipe, dismissedRecipe},
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	updated, err := paramsForAction(t.Context(), originHash, "user-1", "make it vegetarian", s.recipeio)
	if err != nil {
		t.Fatalf("paramsForAction failed: %v", err)
	}

	if updated.Instructions != "make it vegetarian" {
		t.Fatalf("expected instructions to update, got %q", updated.Instructions)
	}
	if len(updated.Saved) != 1 || updated.Saved[0].ComputeHash() != savedRecipe.ComputeHash() {
		t.Fatalf("expected saved recipes from params to persist, got %#v", updated.Saved)
	}
	if len(updated.Dismissed) != 0 {
		t.Fatalf("expected dismissed recipes from params to be dropped, got %#v", updated.Dismissed)
	}
}

func TestParamsForAction_MergesSelectionAndRemovesOppositeRecipes(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved"}
	dismissedRecipe := ai.Recipe{Title: "Dismissed Recipe", Description: "Dismissed"}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	p.Saved = []ai.Recipe{savedRecipe}
	p.Dismissed = []ai.Recipe{dismissedRecipe}
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes: []ai.Recipe{savedRecipe, dismissedRecipe},
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	if err := s.saveRecipeSelection(t.Context(), "user-1", originHash, recipeSelection{
		SavedHashes:     []string{dismissedRecipe.ComputeHash()},
		DismissedHashes: []string{savedRecipe.ComputeHash()},
	}); err != nil {
		t.Fatalf("failed to save selection: %v", err)
	}

	updated, err := paramsForAction(t.Context(), originHash, "user-1", "", s.recipeio)
	if err != nil {
		t.Fatalf("paramsForAction failed: %v", err)
	}

	if len(updated.Saved) != 1 || updated.Saved[0].ComputeHash() != dismissedRecipe.ComputeHash() {
		t.Fatalf("expected selection to move dismissed recipe into saved, got %#v", updated.Saved)
	}
	if len(updated.Dismissed) != 1 || updated.Dismissed[0].ComputeHash() != savedRecipe.ComputeHash() {
		t.Fatalf("expected selection to move saved recipe into dismissed, got %#v", updated.Dismissed)
	}
}

func TestHandleFeedback_CookedButtonSavesCookedState(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	form := url.Values{
		"cooked": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/feedback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleFeedback(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Saved") {
		t.Fatalf("expected success message, got body: %s", rr.Body.String())
	}

	feedback, err := s.FeedbackFromCache(t.Context(), "hash")
	if err != nil {
		t.Fatalf("expected feedback to be saved: %v", err)
	}
	if !feedback.Cooked {
		t.Fatal("expected cooked=true")
	}
	if feedback.Stars != 0 {
		t.Fatalf("expected stars=0, got %d", feedback.Stars)
	}
	if feedback.Comment != "" {
		t.Fatalf("expected empty comment, got %q", feedback.Comment)
	}
}

func TestHandleFeedback_SavesStarsAndComment(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	form := url.Values{
		"cooked":   {"true"},
		"stars":    {"4"},
		"feedback": {"Great flavor and easy cleanup."},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/feedback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleFeedback(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	feedback, err := s.FeedbackFromCache(t.Context(), "hash")
	if err != nil {
		t.Fatalf("expected feedback to be saved: %v", err)
	}
	if !feedback.Cooked {
		t.Fatal("expected cooked=true")
	}
	if feedback.Stars != 4 {
		t.Fatalf("expected stars=4, got %d", feedback.Stars)
	}
	if feedback.Comment != "Great flavor and easy cleanup." {
		t.Fatalf("unexpected comment: %q", feedback.Comment)
	}
}

func TestHandleFeedback_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	form := url.Values{
		"cooked": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/feedback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleFeedback(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got, want := rr.Header().Get("HX-Redirect"), signInPath("/recipe/hash/feedback"); got != want {
		t.Fatalf("expected HX-Redirect %q, got %q", want, got)
	}
}

func TestHandleFeedback_InvalidStarsRejected(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	form := url.Values{
		"cooked": {"true"},
		"stars":  {"7"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/feedback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleFeedback(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestHandleFeedback_RejectsNonHTMXRequest(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	form := url.Values{
		"cooked": {"true"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/feedback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleFeedback(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}
