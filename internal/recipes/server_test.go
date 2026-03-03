package recipes

import (
	"bytes"
	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/users"
	utypes "careme/internal/users/types"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	p := DefaultParams(&locations.Location{ID: "loc-123", Name: "Test"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))
	hash := p.Hash()
	legacyHash, ok := legacyRecipeHash(hash)
	if !ok {
		t.Fatal("expected to derive legacy recipe hash")
	}

	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+url.QueryEscape(legacyHash), nil)
	rr := httptest.NewRecorder()

	s := &server{}
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

func TestHandleRecipes_RedirectsLegacyHashAndPreservesQuery(t *testing.T) {
	p := DefaultParams(&locations.Location{ID: "loc-abc", Name: "Test"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))
	hash := p.Hash()
	legacyHash, ok := legacyRecipeHash(hash)
	if !ok {
		t.Fatal("expected to derive legacy recipe hash")
	}

	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+url.QueryEscape(legacyHash)+"&mail=true&start=2026-01-25T00%3A00%3A00Z", nil)
	rr := httptest.NewRecorder()

	s := &server{}
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

func TestHandleSingle_NormalizesLegacyOriginHashToCanonicalHash(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    auth.DefaultMock(),
	}

	p := DefaultParams(
		&locations.Location{ID: "loc-legacy-origin", Name: "Canonical Test Store"},
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
	}
	recipeHash := recipe.ComputeHash()
	if err := s.SaveRecipes(t.Context(), []ai.Recipe{recipe}, legacyHash); err != nil {
		t.Fatalf("failed to save recipe with legacy origin hash: %v", err)
	}

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
}

func TestHandleSingle_LegacyOriginHashDoesNotFailWhenParamsMissing(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    auth.DefaultMock(),
	}

	p := DefaultParams(
		&locations.Location{ID: "loc-legacy-origin-missing-params", Name: "Ignored"},
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
	if err := s.SaveRecipes(t.Context(), []ai.Recipe{recipe}, legacyHash); err != nil {
		t.Fatalf("failed to save recipe with legacy origin hash: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/recipe/"+recipeHash, nil)
	req.SetPathValue("hash", recipeHash)
	rr := httptest.NewRecorder()

	s.handleSingle(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "/recipes?h="+canonicalHash) {
		t.Fatalf("expected canonical back-link hash %q in response body: %s", canonicalHash, body)
	}
	if !strings.Contains(body, "Unknown Location") {
		t.Fatalf("expected fallback params rendering with Unknown Location, body: %s", body)
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

func (n noSessionAuth) Register(mux *http.ServeMux) {}

func TestHandleQuestion_RequiresSignedInUser(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    noSessionAuth{},
	}

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
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    auth.DefaultMock(),
	}

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

type captureQuestionGenerator struct {
	lastQuestion string
}

func (c *captureQuestionGenerator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	return &ai.ShoppingList{}, nil
}

func (c *captureQuestionGenerator) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	c.lastQuestion = question
	return "Try chicken thighs at the same cook time.", nil
}

func (c *captureQuestionGenerator) Ready(ctx context.Context) error {
	return nil
}

func TestHandleQuestion_HTMXReturnsThreadFragment(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio:  recipeio{Cache: cacheStore},
		storage:   users.NewStorage(cacheStore),
		clerk:     auth.DefaultMock(),
		generator: &captureQuestionGenerator{},
	}

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
}

func TestHandleQuestion_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    noSessionAuth{},
	}

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
	if got := rr.Header().Get("HX-Redirect"); got != "/sign-in" {
		t.Fatalf("expected HX-Redirect to /, got %q", got)
	}
}

func TestHandleQuestion_PrependsRecipeTitleForModelQuestion(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	g := &captureQuestionGenerator{}
	s := &server{
		recipeio:  recipeio{Cache: cacheStore},
		storage:   users.NewStorage(cacheStore),
		clerk:     auth.DefaultMock(),
		generator: g,
	}

	form := url.Values{
		"conversation_id": {"conv-test"},
		"question":        {"Can I swap the protein?"},
		"recipe_title":    {"BBQ Pulled Pork"},
	}
	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/question", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleQuestion(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if got, want := g.lastQuestion, "Regarding BBQ Pulled Pork: Can I swap the protein?"; got != want {
		t.Fatalf("expected generator question %q, got %q", want, got)
	}
}

func TestHandleSaveRecipe_SavesRecipeToUserProfile(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  storage,
		clerk:    auth.DefaultMock(),
	}

	recipe := ai.Recipe{
		Title:       "Save Me",
		Description: "Recipe to save",
	}
	recipeHash := recipe.ComputeHash()
	if err := s.SaveRecipes(t.Context(), []ai.Recipe{recipe}, "origin-hash"); err != nil {
		t.Fatalf("failed to save recipe in cache: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/save", nil)
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
	selection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", "origin-hash")
	if err != nil {
		t.Fatalf("failed to load selection: %v", err)
	}
	if len(selection.SavedHashes) != 1 || selection.SavedHashes[0] != recipeHash {
		t.Fatalf("expected saved selection with hash %q, got %#v", recipeHash, selection.SavedHashes)
	}
}

func TestHandleSaveRecipe_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    noSessionAuth{},
	}

	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/save", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleSaveRecipe(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/sign-in" {
		t.Fatalf("expected HX-Redirect to /sign-in, got %q", got)
	}
}

func TestHandleDismissRecipe_RemovesRecipeFromUserProfile(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  storage,
		clerk:    auth.DefaultMock(),
	}

	recipe := ai.Recipe{
		Title:       "Dismiss Recipe",
		Description: "Recipe to dismiss",
	}
	recipeHash := recipe.ComputeHash()
	if err := s.SaveRecipes(t.Context(), []ai.Recipe{recipe}, "origin-hash"); err != nil {
		t.Fatalf("failed to save recipe in cache: %v", err)
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

	req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipeHash+"/dismiss", nil)
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
	selection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", "origin-hash")
	if err != nil {
		t.Fatalf("failed to load selection: %v", err)
	}
	if len(selection.DismissedHashes) != 1 || selection.DismissedHashes[0] != recipeHash {
		t.Fatalf("expected dismissed selection with hash %q, got %#v", recipeHash, selection.DismissedHashes)
	}
}

func TestHandleDismissRecipe_NoSessionHTMXSetsRedirectHeader(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    noSessionAuth{},
	}

	req := httptest.NewRequest(http.MethodPost, "/recipe/hash/dismiss", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("hash", "hash")
	rr := httptest.NewRecorder()

	s.handleDismissRecipe(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
	if got := rr.Header().Get("HX-Redirect"); got != "/sign-in" {
		t.Fatalf("expected HX-Redirect to /sign-in, got %q", got)
	}
}

func TestHandleRegenerate_UsesServerSideSelectionAndRedirects(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := &server{
		recipeio:  recipeio{Cache: cacheStore},
		storage:   storage,
		clerk:     auth.DefaultMock(),
		generator: mock{},
	}
	t.Cleanup(s.Wait)

	p := DefaultParams(&locations.Location{ID: "loc-1", Name: "Store"}, time.Now())
	p.ConversationID = "conv-123"
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved"}
	dismissedRecipe := ai.Recipe{Title: "Dismissed Recipe", Description: "Dismissed"}
	if err := s.SaveRecipes(t.Context(), []ai.Recipe{savedRecipe, dismissedRecipe}, originHash); err != nil {
		t.Fatalf("failed to save recipes: %v", err)
	}
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

func TestHandleFinalize_UsesServerSideSelection(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := users.NewStorage(cacheStore)
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  storage,
		clerk:    auth.DefaultMock(),
	}

	p := DefaultParams(&locations.Location{ID: "loc-1", Name: "Store"}, time.Now())
	p.ConversationID = "conv-123"
	originHash := p.Hash()
	if err := s.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("failed to save params: %v", err)
	}

	savedRecipe := ai.Recipe{Title: "Saved Recipe", Description: "Saved"}
	dismissedRecipe := ai.Recipe{Title: "Dismissed Recipe", Description: "Dismissed"}
	if err := s.SaveRecipes(t.Context(), []ai.Recipe{savedRecipe, dismissedRecipe}, originHash); err != nil {
		t.Fatalf("failed to save recipes: %v", err)
	}
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

func TestHandleFeedback_CookedButtonSavesCookedState(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    auth.DefaultMock(),
	}

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
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    auth.DefaultMock(),
	}

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

func TestHandleFeedback_InvalidStarsRejected(t *testing.T) {
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    auth.DefaultMock(),
	}

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
	s := &server{
		recipeio: recipeio{Cache: cacheStore},
		storage:  users.NewStorage(cacheStore),
		clerk:    auth.DefaultMock(),
	}

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
