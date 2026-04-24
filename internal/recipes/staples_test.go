package recipes

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/albertsons"
	"careme/internal/cache"
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
	ingredients []kroger.Ingredient
	fn          func([]kroger.Ingredient) ([]ai.InputIngredient, error)
	err         error
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

func (s *stubIngredientGrader) GradeIngredients(_ context.Context, ingredients []kroger.Ingredient) ([]ai.InputIngredient, error) {
	s.ingredients = append([]kroger.Ingredient(nil), ingredients...)
	results := make([]ai.InputIngredient, 0, len(ingredients))
	if s.err != nil {
		return nil, s.err
	}
	if s.fn != nil {
		return s.fn(ingredients)
	}
	for _, ingredient := range ingredients {
		results = append(results, ai.InputIngredient{
			ProductID:   toValue(ingredient.ProductId),
			Description: toValue(ingredient.Description),
			Brand:       toValue(ingredient.Brand),
			Size:        toValue(ingredient.Size),
			Grade:       &ai.IngredientGrade{SchemaVersion: "ingredient-grade-v1", Score: 10, Reason: "stub"},
		})
	}
	return results, nil
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
			{ProductId: loPtr("apple-1"), Description: loPtr("Honeycrisp Apple")},
			{ProductId: loPtr("apple-1"), Description: loPtr("Honeycrisp Apple")},
			{ProductId: loPtr("spinach-1"), Description: loPtr("Baby Spinach")},
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
	if got[0].ProductID == "" {
		t.Fatalf("expected input ingredient product id, got %+v", got)
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

func TestGetStaples_GradesCachedIngredientsBeforeReturning(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	grader := &stubIngredientGrader{}
	steak := kroger.Ingredient{ProductId: loPtr("steak-1"), Description: loPtr("Ribeye Steak")}
	chips := kroger.Ingredient{ProductId: loPtr("chips-1"), Description: loPtr("Potato Chips")}
	s := &cachedStaplesService{
		cache:  IO(cacheStore),
		grader: grader,
		provider: &stubRoutingStaplesProvider{
			ingredients: []kroger.Ingredient{chips, steak},
		},
	}

	params := &generatorParams{
		Location: &locations.Location{ID: "70100023", Name: "Test Store"},
		Date:     time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}

	got, err := s.GetStaples(t.Context(), params)
	if err != nil {
		t.Fatalf("GetStaples returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected graded staples length: %+v", got)
	}
	slices.SortFunc(got, func(a, b ai.InputIngredient) int {
		return strings.Compare(a.Description, b.Description)
	})
	if got[0].Description != "Potato Chips" || got[0].Grade == nil || got[0].Grade.Score != 10 {
		t.Fatalf("unexpected graded staple entry: %+v", got[0])
	}
	if got[1].Description != "Ribeye Steak" || got[1].Grade == nil || got[1].Grade.Score != 10 {
		t.Fatalf("unexpected graded staple entry: %+v", got[1])
	}
	if len(grader.ingredients) != 2 {
		t.Fatalf("expected grader to see raw cached ingredients, got %d", len(grader.ingredients))
	}

	cached, err := IO(cacheStore).IngredientsFromCache(t.Context(), params.LocationHash())
	if err != nil {
		t.Fatalf("IngredientsFromCache returned error: %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("expected cached ingredients, got %+v", cached)
	}
	slices.SortFunc(cached, func(a, b ai.InputIngredient) int {
		return strings.Compare(a.Description, b.Description)
	})
	if cached[0].Description != "Potato Chips" || cached[0].Grade == nil || cached[0].Grade.Score != 10 {
		t.Fatalf("expected graded chips in cache, got %+v", cached[0])
	}
	if cached[1].Description != "Ribeye Steak" || cached[1].Grade == nil || cached[1].Grade.Score != 10 {
		t.Fatalf("expected graded steak in cache, got %+v", cached[1])
	}
}

func toValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
