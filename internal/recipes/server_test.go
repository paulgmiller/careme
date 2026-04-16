package recipes

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
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
	"careme/internal/locations"
	"careme/internal/recipes/feedback"
	"careme/internal/routing"
	"careme/internal/users"

	utypes "careme/internal/users/types"
)

func TestRedirectToHash(t *testing.T) {
	// Create a ResponseRecorder to record the response
	rr := httptest.NewRecorder()
	// Create a dummy request
	req := httptest.NewRequest("GET", "/dummy", nil)

	hash := "testhash"
	redirectToHash(rr, req, hash, true)

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

func TestHandleRecipes_GuestRedirectsToSignInWhenCacheMisses(t *testing.T) {
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
	p.ConversationID = "conv-canonical"
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
	p.ConversationID = "conv-wine-single"
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
		"conversation_id": {"conv-test"},
		"question":        {"Can I swap the protein?"},
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
		"conversation_id": {"conv-test"},
		"question":        {"Can I swap the protein?"},
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

type captureKickgenerationGenerator struct {
	mu     sync.Mutex
	last   *generatorParams
	called chan struct{}
}

func (c *captureKickgenerationGenerator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	c.mu.Lock()
	clone := *p
	clone.LastRecipes = append([]string(nil), p.LastRecipes...)
	clone.PriorSavedHashes = append([]string(nil), p.PriorSavedHashes...)
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
	return &ai.ShoppingList{}, nil
}

func (c *captureKickgenerationGenerator) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
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
	manualRecent := utypes.Recipe{Title: "Manual Recently", CreatedAt: now.Add(-36 * time.Hour)}
	notCookedRecent := utypes.Recipe{Title: "Only Saved", Hash: "hash-saved-recent", CreatedAt: now.Add(-24 * time.Hour)}
	tooOldCooked := utypes.Recipe{Title: "Cooked Too Old", Hash: "hash-cooked-old", CreatedAt: now.Add(-15 * 24 * time.Hour)}
	currentUser := &utypes.User{
		ID:          "user-1",
		Email:       []string{"chef@example.com"},
		ShoppingDay: "Saturday",
		LastRecipes: []utypes.Recipe{cookedRecent, manualRecent, notCookedRecent, tooOldCooked},
	}

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
	s.kickgeneration(t.Context(), params, currentUser)

	select {
	case <-generator.called:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for generator call")
	}

	captured := generator.LastParams()
	if captured == nil {
		t.Fatal("expected captured params")
	}
	if got, want := captured.LastRecipes, []string{"Cooked Recently", "Manual Recently"}; !slices.Equal(got, want) {
		t.Fatalf("expected cooked and manual recent recipes in avoid list, got %v", got)
	}
}

type captureQuestionGenerator struct {
	lastQuestion       string
	lastConversationID string
	lastWinePick       struct {
		recipeTitle string
		date        time.Time
	}
	wineRecommendation string
	winePickCalls      int
	panicOnWine        bool
	imageCalls         int
	panicOnImage       bool
	imageBody          []byte
}

func (c *captureQuestionGenerator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	return &ai.ShoppingList{}, nil
}

func (c *captureQuestionGenerator) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	c.lastQuestion = question
	c.lastConversationID = conversationID
	return "Try chicken thighs at the same cook time.", nil
}

func (c *captureQuestionGenerator) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
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

func seedQuestionConversation(t *testing.T, s *server, conversationID string) string {
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
		Recipes:        []ai.Recipe{recipe},
		ConversationID: conversationID,
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

	recipeHash := seedQuestionConversation(t, s, "conv-test")

	form := url.Values{
		"conversation_id": {"conv-test"},
		"question":        {"Can I swap the protein?"},
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
	if got, want := s.generator.(*captureQuestionGenerator).lastConversationID, "conv-test"; got != want {
		t.Fatalf("expected generator conversation ID %q, got %q", want, got)
	}
}

func TestHandleQuestion_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	form := url.Values{
		"conversation_id": {"conv-test"},
		"question":        {"Can I swap the protein?"},
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

	recipeHash := seedQuestionConversation(t, s, "conv-test")

	form := url.Values{
		"conversation_id": {"conv-test"},
		"question":        {"Can I swap the protein?"},
		"recipe_title":    {"BBQ Pulled Pork"},
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

func TestHandleWine_RejectsNonHTMXRequest(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore))

	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/wine", nil)
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleWine(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestHandleWine_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/wine?view=shopping", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleWine(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got, want := rr.Header().Get("HX-Redirect"), signInPath("/recipe/hash/wine?view=shopping"); got != want {
		t.Fatalf("expected HX-Redirect %q, got %q", want, got)
	}
}

func TestHandleWine_HTMXReturnsWineFragment(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	g := &captureQuestionGenerator{}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(g),
	)

	p := DefaultParams(&locations.Location{ID: "70003002", Name: "Wine Test Store"}, time.Now())
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
		WineStyles:   []string{"pinot noir"},
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)

	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/wine", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleWine(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `id="wine-recommendation"`) {
		t.Fatalf("expected wine fragment container in response, got body: %s", body)
	}
	if !strings.Contains(body, "Try a chilled sauvignon blanc.") {
		t.Fatalf("expected wine recommendation in response, got body: %s", body)
	}
	if got, want := g.lastWinePick.recipeTitle, "Roast Chicken"; got != want {
		t.Fatalf("expected recipe title %q, got %q", want, got)
	}
	if got, want := g.lastWinePick.date.Format("2006-01-02"), p.Date.Format("2006-01-02"); got != want {
		t.Fatalf("expected wine date %q, got %q", want, got)
	}
	if got, want := g.winePickCalls, 1; got != want {
		t.Fatalf("expected PickAWine call count %d, got %d", want, got)
	}
}

func TestHandleWine_ShoppingVariantReturnsShoppingFragment(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	g := &captureQuestionGenerator{}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(g),
	)

	p := DefaultParams(&locations.Location{ID: "70003002", Name: "Wine Test Store"}, time.Now())
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
		WineStyles:   []string{"pinot noir"},
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)

	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/wine?view=shopping", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleWine(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	actionID, _ := shoppingWineDOMIDs(recipeHash)
	previewID := shoppingWinePreviewDOMID(recipeHash)
	detailContainerID, _ := shoppingWineDetailDOMIDs(recipeHash)
	if !strings.Contains(body, `id="`+actionID+`"`) {
		t.Fatalf("expected shopping wine fragment container in response, got body: %s", body)
	}
	if !strings.Contains(body, `id="`+previewID+`"`) || !strings.Contains(body, `id="`+detailContainerID+`"`) || !strings.Contains(body, `hx-swap-oob="outerHTML"`) {
		t.Fatalf("expected shopping wine response to update preview and details containers out-of-band, got body: %s", body)
	}
	if !strings.Contains(body, "Try a chilled sauvignon blanc.") {
		t.Fatalf("expected wine recommendation in response, got body: %s", body)
	}
}

func TestHandleWine_UsesCachedWineRecommendation(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	g1 := &captureQuestionGenerator{wineRecommendation: "Try a crisp riesling."}
	s1 := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(g1),
	)

	p := DefaultParams(&locations.Location{ID: "70003002", Name: "Wine Test Store"}, time.Now())
	originHash := p.Hash()
	if err := s1.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipe := ai.Recipe{
		OriginHash:   originHash,
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		Ingredients:  []ai.Ingredient{{Name: "chicken", Quantity: "1", Price: "$12"}},
		Instructions: []string{"Roast until done."},
		WineStyles:   []string{"pinot noir"},
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s1, originHash, recipe)

	req1 := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/wine", nil)
	req1.Header.Set("HX-Request", "true")
	req1.SetPathValue("hash", recipeHash)
	rr1 := httptest.NewRecorder()
	s1.handleWine(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body: %s", http.StatusOK, rr1.Code, rr1.Body.String())
	}
	if !strings.Contains(rr1.Body.String(), "Try a crisp riesling.") {
		t.Fatalf("expected initial recommendation in response, got body: %s", rr1.Body.String())
	}
	if got, want := g1.winePickCalls, 1; got != want {
		t.Fatalf("expected PickAWine call count %d, got %d", want, got)
	}

	g2 := &captureQuestionGenerator{panicOnWine: true}
	s2 := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(g2),
	)

	req2 := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/wine", nil)
	req2.Header.Set("HX-Request", "true")
	req2.SetPathValue("hash", recipeHash)
	rr2 := httptest.NewRecorder()
	s2.handleWine(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body: %s", http.StatusOK, rr2.Code, rr2.Body.String())
	}
	if !strings.Contains(rr2.Body.String(), "Try a crisp riesling.") {
		t.Fatalf("expected cached recommendation in response, got body: %s", rr2.Body.String())
	}
	if got, want := g2.winePickCalls, 0; got != want {
		t.Fatalf("expected PickAWine call count %d, got %d", want, got)
	}
}

func TestHandleRecipeImage_ServesCachedImageWithoutGenerator(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	g := &captureQuestionGenerator{panicOnImage: true}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(g),
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

func TestHandleGenerateRecipeImage_GeneratesAndCachesOnMiss(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	g := &captureQuestionGenerator{imageBody: []byte("generated-image-bytes")}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestGenerator(g),
	)

	recipe := ai.Recipe{
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		Ingredients:  []ai.Ingredient{{Name: "chicken", Quantity: "1", Price: "$12"}},
		Instructions: []string{"Roast until done."},
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, "origin-hash", recipe)

	req1 := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/image", nil)
	req1.Header.Set("HX-Request", "true")
	req1.SetPathValue("hash", recipeHash)
	rr1 := httptest.NewRecorder()

	s.handleGenerateRecipeImage(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body: %s", http.StatusOK, rr1.Code, rr1.Body.String())
	}
	body := rr1.Body.String()
	if !strings.Contains(body, `id="recipe-image-action"`) {
		t.Fatalf("expected recipe image action fragment, got body: %s", body)
	}
	if !strings.Contains(body, `id="recipe-image-panel"`) || !strings.Contains(body, `hx-swap-oob="outerHTML"`) {
		t.Fatalf("expected recipe image panel out-of-band update, got body: %s", body)
	}
	if !strings.Contains(body, "/recipe/"+recipeHash+"/image") {
		t.Fatalf("expected recipe image URL in response, got body: %s", body)
	}
	if got, want := g.imageCalls, 1; got != want {
		t.Fatalf("expected GenerateRecipeImage call count %d, got %d", want, got)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/image", nil)
	req2.Header.Set("HX-Request", "true")
	req2.SetPathValue("hash", recipeHash)
	rr2 := httptest.NewRecorder()

	s.handleGenerateRecipeImage(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body: %s", http.StatusOK, rr2.Code, rr2.Body.String())
	}
	if got, want := g.imageCalls, 1; got != want {
		t.Fatalf("expected cached image to avoid extra generation; got %d calls", got)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/recipe/"+recipeHash+"/image", nil)
	req3.SetPathValue("hash", recipeHash)
	rr3 := httptest.NewRecorder()

	s.handleRecipeImage(rr3, req3)

	if rr3.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body: %s", http.StatusOK, rr3.Code, rr3.Body.String())
	}
	if got := rr3.Body.String(); got != "generated-image-bytes" {
		t.Fatalf("expected cached generated image body, got %q", got)
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
	}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	p.ConversationID = "conv-123"
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes:        []ai.Recipe{recipe},
		ConversationID: "conv-123",
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

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Saved to kitchen") {
		t.Fatalf("expected success response, got body: %s", rr.Body.String())
	}
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

func TestHandleSaveRecipe_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := newTestServer(t, withTestCache(cacheStore), withTestClerk(noSessionAuth{}))

	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/save", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got, want := rr.Header().Get("HX-Redirect"), signInPath("/recipe/hash/save"); got != want {
		t.Fatalf("expected HX-Redirect %q, got %q", want, got)
	}
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
	}
	currentParams := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	currentParams.ConversationID = "conv-123"
	currentHash := currentParams.Hash()
	if err := s.SaveParams(t.Context(), currentParams); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, "stale-origin-hash", recipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes:        []ai.Recipe{recipe},
		ConversationID: "conv-123",
	}, currentHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/save?h="+currentHash, nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)

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
	}
	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	p.ConversationID = "conv-123"
	p.Saved = []ai.Recipe{recipe}
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, originHash, recipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes:        []ai.Recipe{recipe},
		ConversationID: "conv-123",
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
	if !strings.Contains(rr.Body.String(), "Removed from kitchen") {
		t.Fatalf("expected dismiss response, got body: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `id="shopping-finalize-controls"`) || !strings.Contains(rr.Body.String(), `hx-swap-oob="outerHTML"`) {
		t.Fatalf("expected finalize controls oob response, got body: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `disabled`) || !strings.Contains(rr.Body.String(), `Save at least one recipe to assemble your shopping list.`) {
		t.Fatalf("expected finalize button to become disabled after dismiss, got body: %s", rr.Body.String())
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
	}
	currentParams := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	currentParams.ConversationID = "conv-123"
	currentHash := currentParams.Hash()
	if err := s.SaveParams(t.Context(), currentParams); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}
	recipeHash := recipe.ComputeHash()
	saveRecipesForOrigin(t, s, "stale-origin-hash", recipe)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes:        []ai.Recipe{recipe},
		ConversationID: "conv-123",
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
	p.ConversationID = "conv-123"
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved"}
	dismissedRecipe := ai.Recipe{Title: "Dismissed Recipe", Description: "Dismissed"}
	saveRecipesForOrigin(t, s, originHash, savedRecipe, dismissedRecipe)
	shoppingList := &ai.ShoppingList{
		Recipes:        []ai.Recipe{savedRecipe, dismissedRecipe},
		ConversationID: "conv-123",
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

func TestHandleRegenerate_PassesPriorSavedHashesToGenerator(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t,
		withTestCache(cacheStore),
		withTestStorage(storage),
		withTestGenerator(generator),
	)
	t.Cleanup(s.Wait)

	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier"}
	newlySaved := ai.Recipe{Title: "Newly Saved", Description: "Saved now"}
	available := ai.Recipe{Title: "Still Available", Description: "Fresh"}

	p := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	p.ConversationID = "conv-123"
	p.Saved = []ai.Recipe{alreadySaved}
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	saveRecipesForOrigin(t, s, originHash, alreadySaved, newlySaved, available)
	if err := s.SaveShoppingList(t.Context(), &ai.ShoppingList{
		Recipes:        []ai.Recipe{alreadySaved, newlySaved, available},
		ConversationID: "conv-123",
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
	if captured == nil {
		t.Fatal("expected captured params")
	}
	if got, want := captured.PriorSavedHashes, []string{alreadySaved.ComputeHash()}; !slices.Equal(got, want) {
		t.Fatalf("expected prior saved hashes %v, got %v", want, got)
	}
	if len(captured.Saved) != 2 {
		t.Fatalf("expected both current saved recipes, got %#v", captured.Saved)
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
	p.ConversationID = "conv-123"
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved"}
	dismissedRecipe := ai.Recipe{Title: "Dismissed Recipe", Description: "Dismissed"}
	saveRecipesForOrigin(t, s, originHash, savedRecipe, dismissedRecipe)
	shoppingList := &ai.ShoppingList{
		Recipes:        []ai.Recipe{savedRecipe, dismissedRecipe},
		ConversationID: "conv-123",
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
}

func TestParamsForAction_PreservesBaseSelectionWhenSelectionCacheEmpty(t *testing.T) {
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
		Recipes:        []ai.Recipe{savedRecipe, dismissedRecipe},
		ConversationID: "conv-1",
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	updated, err := s.paramsForAction(t.Context(), originHash, "user-1", "make it vegetarian")
	if err != nil {
		t.Fatalf("paramsForAction failed: %v", err)
	}

	if updated.Instructions != "make it vegetarian" {
		t.Fatalf("expected instructions to update, got %q", updated.Instructions)
	}
	if len(updated.Saved) != 1 || updated.Saved[0].ComputeHash() != savedRecipe.ComputeHash() {
		t.Fatalf("expected saved recipes from params to persist, got %#v", updated.Saved)
	}
	if len(updated.Dismissed) != 1 || updated.Dismissed[0].ComputeHash() != dismissedRecipe.ComputeHash() {
		t.Fatalf("expected dismissed recipes from params to persist, got %#v", updated.Dismissed)
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
		Recipes:        []ai.Recipe{savedRecipe, dismissedRecipe},
		ConversationID: "conv-1",
	}, originHash); err != nil {
		t.Fatalf("failed to save shopping list: %v", err)
	}

	if err := s.saveRecipeSelection(t.Context(), "user-1", originHash, recipeSelection{
		SavedHashes:     []string{dismissedRecipe.ComputeHash()},
		DismissedHashes: []string{savedRecipe.ComputeHash()},
	}); err != nil {
		t.Fatalf("failed to save selection: %v", err)
	}

	updated, err := s.paramsForAction(t.Context(), originHash, "user-1", "")
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
