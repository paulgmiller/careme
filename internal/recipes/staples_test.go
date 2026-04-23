package recipes

import (
	"context"
	"slices"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/albertsons"
	"careme/internal/cache"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/kroger"
	"careme/internal/locations"
)

type stubStaplesProvider struct {
	ids         map[string]bool
	ingredients []kroger.Ingredient
	err         error
	calls       int
}

func (s *stubStaplesProvider) IsID(locationID string) bool {
	return s.ids[locationID]
}

func (s *stubStaplesProvider) Signature() string {
	return "stub-staples-v1"
}

func (s *stubStaplesProvider) FetchStaples(_ context.Context, _ string) ([]kroger.Ingredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

func (s *stubStaplesProvider) GetIngredients(_ context.Context, _ string, _ string, _ int) ([]kroger.Ingredient, error) {
	return s.FetchStaples(context.Background(), "")
}

type stubRoutingStaplesProvider struct {
	ingredients []kroger.Ingredient
	err         error
	calls       int
}

type stubIngredientGrader struct {
	locationHash string
	ingredients  []kroger.Ingredient
	prioritized  []kroger.Ingredient
	err          error
}

func (s *stubRoutingStaplesProvider) FetchStaples(_ context.Context, _ string) ([]kroger.Ingredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

func (s *stubRoutingStaplesProvider) GetIngredients(_ context.Context, _ string, _ string, _ int) ([]kroger.Ingredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

func (s *stubIngredientGrader) GradeIngredients(_ context.Context, locationHash string, ingredients []kroger.Ingredient) <-chan ingredientgrading.Result {
	results := make(chan ingredientgrading.Result, len(ingredients))
	for _, ingredient := range ingredients {
		results <- ingredientgrading.Result{
			Ingredient: ingredient,
			Grade: &ai.IngredientGrade{
				SchemaVersion: "ingredient-grade-v1",
				Score:         10,
				Reason:        "stub",
				Ingredient:    ai.SnapshotFromKrogerIngredient(ingredient),
			},
		}
	}
	close(results)
	return results
}

func (s *stubIngredientGrader) PrioritizeIngredients(_ context.Context, locationHash string, ingredients []kroger.Ingredient) ([]kroger.Ingredient, error) {
	s.locationHash = locationHash
	s.ingredients = append([]kroger.Ingredient(nil), ingredients...)
	if s.err != nil {
		return nil, s.err
	}
	if s.prioritized != nil {
		return append([]kroger.Ingredient(nil), s.prioritized...), nil
	}
	return append([]kroger.Ingredient(nil), ingredients...), nil
}

func TestRoutingStaplesProvider_SelectsProviderByLocationID(t *testing.T) {
	krogerProvider := &stubStaplesProvider{ids: map[string]bool{"70100023": true}}
	wholeFoodsProvider := &stubStaplesProvider{ids: map[string]bool{"wholefoods_10216": true}}
	provider := routingStaplesProvider{
		backends: []backendStaplesProvider{krogerProvider, wholeFoodsProvider},
	}

	if _, err := provider.FetchStaples(t.Context(), "70100023"); err != nil {
		t.Fatalf("FetchStaples kroger returned error: %v", err)
	}
	if krogerProvider.calls != 1 || wholeFoodsProvider.calls != 0 {
		t.Fatalf("expected kroger provider to be selected, got kroger=%d wholefoods=%d", krogerProvider.calls, wholeFoodsProvider.calls)
	}

	if _, err := provider.FetchStaples(t.Context(), "wholefoods_10216"); err != nil {
		t.Fatalf("FetchStaples whole foods returned error: %v", err)
	}
	if krogerProvider.calls != 1 || wholeFoodsProvider.calls != 1 {
		t.Fatalf("expected whole foods provider to be selected once, got kroger=%d wholefoods=%d", krogerProvider.calls, wholeFoodsProvider.calls)
	}
}

func TestRoutingStaplesProvider_RejectsUnsupportedLocationBackend(t *testing.T) {
	provider := routingStaplesProvider{
		backends: []backendStaplesProvider{
			&stubStaplesProvider{ids: map[string]bool{"70100023": true}},
			&stubStaplesProvider{ids: map[string]bool{"wholefoods_10216": true}},
		},
	}

	_, err := provider.FetchStaples(t.Context(), "walmart_3098")
	if err == nil {
		t.Fatal("expected unsupported backend error")
	}
	if got, want := err.Error(), `staples provider does not support location "walmart_3098"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestRoutingStaplesProvider_GetIngredients_SelectsProviderByLocationID(t *testing.T) {
	krogerProvider := &stubStaplesProvider{
		ids:         map[string]bool{"70100023": true},
		ingredients: []kroger.Ingredient{{Description: loPtr("Pinot Noir")}},
	}
	wholeFoodsProvider := &stubStaplesProvider{
		ids:         map[string]bool{"wholefoods_10216": true},
		ingredients: []kroger.Ingredient{{Description: loPtr("Whole Foods Pinot Noir")}},
	}
	provider := routingStaplesProvider{
		backends: []backendStaplesProvider{krogerProvider, wholeFoodsProvider},
	}

	got, err := provider.GetIngredients(t.Context(), "wholefoods_10216", "pinot noir", 0)
	if err != nil {
		t.Fatalf("GetIngredients returned error: %v", err)
	}
	if len(got) != 1 || got[0].Description == nil || *got[0].Description != "Whole Foods Pinot Noir" {
		t.Fatalf("unexpected ingredients: %+v", got)
	}
	if krogerProvider.calls != 0 || wholeFoodsProvider.calls != 1 {
		t.Fatalf("expected whole foods provider to be selected, got kroger=%d wholefoods=%d", krogerProvider.calls, wholeFoodsProvider.calls)
	}
}

func TestStaplesSignatureForLocation_UsesAlbertsonsIdentityProvider(t *testing.T) {
	t.Parallel()

	got := staplesSignatureForLocation("safeway_1142")
	want := albertsons.NewIdentityProvider().Signature()
	if got != want {
		t.Fatalf("unexpected signature: got %q want %q", got, want)
	}
}

func TestGetStaples_UsesProviderAndCachesWholeFoodsResults(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	provider := &stubRoutingStaplesProvider{
		ingredients: []kroger.Ingredient{
			{Description: loPtr("Honeycrisp Apple")},
			{Description: loPtr("Honeycrisp Apple")},
			{Description: loPtr("Baby Spinach")},
		},
	}
	s := &cachedStaplesService{
		cache:    IO(cacheStore),
		provider: provider,
	}

	params := &generatorParams{
		Location: &locations.Location{ID: "wholefoods_10216", Name: "Westlake"},
		Date:     time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
	}

	got, err := s.GetStaples(t.Context(), params)
	if err != nil {
		t.Fatalf("GetStaples returned error: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider to be called once, got %d", provider.calls)
	}
	if len(got) != 2 {
		t.Fatalf("expected deduped results, got %d", len(got))
	}

	cached, err := IO(cacheStore).IngredientsFromCache(t.Context(), params.LocationHash())
	if err != nil {
		t.Fatalf("IngredientsFromCache returned error: %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("expected cached deduped results, got %d", len(cached))
	}

	gotAgain, err := s.GetStaples(t.Context(), params)
	if err != nil {
		t.Fatalf("GetStaples returned error on cached call: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected cached call to skip provider, got %d calls", provider.calls)
	}
	if len(gotAgain) != 2 {
		t.Fatalf("expected cached results, got %d", len(gotAgain))
	}
}

func TestGetStaples_PrioritizesCachedIngredientsBeforeReturning(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	grader := &stubIngredientGrader{}
	steak := kroger.Ingredient{Description: loPtr("Ribeye Steak")}
	chips := kroger.Ingredient{Description: loPtr("Potato Chips")}
	s := &cachedStaplesService{
		cache:  IO(cacheStore),
		grader: grader,
		provider: &stubRoutingStaplesProvider{
			ingredients: []kroger.Ingredient{chips, steak},
		},
	}
	grader.prioritized = []kroger.Ingredient{steak}

	params := &generatorParams{
		Location: &locations.Location{ID: "70100023", Name: "Test Store"},
		Date:     time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}

	got, err := s.GetStaples(t.Context(), params)
	if err != nil {
		t.Fatalf("GetStaples returned error: %v", err)
	}
	if len(got) != 1 || got[0].Description == nil || *got[0].Description != "Ribeye Steak" {
		t.Fatalf("unexpected prioritized staples: %+v", got)
	}
	if grader.locationHash != params.LocationHash() {
		t.Fatalf("unexpected location hash: got %q want %q", grader.locationHash, params.LocationHash())
	}
	if len(grader.ingredients) != 2 {
		t.Fatalf("expected grader to see raw cached ingredients, got %d", len(grader.ingredients))
	}

	cached, err := IO(cacheStore).IngredientsFromCache(t.Context(), params.LocationHash())
	if err != nil {
		t.Fatalf("IngredientsFromCache returned error: %v", err)
	}
	if len(cached) != 2 || cached[0].Description == nil || *cached[0].Description != "Potato Chips" {
		t.Fatalf("expected raw ingredients to stay cached, got %+v", cached)
	}
}
