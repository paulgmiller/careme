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
	"careme/internal/locations"
)

type stubStaplesProvider struct {
	ids         map[string]bool
	ingredients []ai.InputIngredient
	err         error
	calls       int
}

func (s *stubStaplesProvider) IsID(locationID string) bool {
	return s.ids[locationID]
}

func (s *stubStaplesProvider) Signature() string {
	return "stub-staples-v1"
}

func (s *stubStaplesProvider) FetchStaples(_ context.Context, _ string) ([]ai.InputIngredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

func (s *stubStaplesProvider) GetIngredients(_ context.Context, _ string, _ string, _ int) ([]ai.InputIngredient, error) {
	return s.FetchStaples(context.Background(), "")
}

func (s *stubStaplesProvider) FetchWines(_ context.Context, _ string, _ []string) ([]ai.InputIngredient, error) {
	return s.FetchStaples(context.Background(), "")
}

type stubRoutingStaplesProvider struct {
	ingredients []ai.InputIngredient
	err         error
	calls       int
}

type stubIngredientGrader struct {
	ingredients []ai.InputIngredient
	fn          func([]ai.InputIngredient) ([]ai.InputIngredient, error)
	err         error
}

func (s *stubRoutingStaplesProvider) FetchStaples(_ context.Context, _ string) ([]ai.InputIngredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

func (s *stubRoutingStaplesProvider) GetIngredients(_ context.Context, _ string, _ string, _ int) ([]ai.InputIngredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

func (s *stubRoutingStaplesProvider) FetchWines(_ context.Context, _ string, _ []string) ([]ai.InputIngredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

func (s *stubIngredientGrader) GradeIngredients(_ context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	s.ingredients = append([]ai.InputIngredient(nil), ingredients...)
	results := make([]ai.InputIngredient, 0, len(ingredients))
	if s.err != nil {
		return nil, s.err
	}
	if s.fn != nil {
		return s.fn(ingredients)
	}
	for _, ingredient := range ingredients {
		ingredient.Grade = &ai.IngredientGrade{Score: 10, Reason: "stub"}
		results = append(results, ingredient)
	}
	return results, nil
}

func TestRoutingStaplesProvider_SelectsProviderByLocationID(t *testing.T) {
	krogerBackend := &stubStaplesProvider{ids: map[string]bool{"70100023": true}}
	wholeFoodsProvider := &stubStaplesProvider{ids: map[string]bool{"wholefoods_10216": true}}
	provider := routingStaplesProvider{
		backends: []backendStaplesProvider{krogerBackend, wholeFoodsProvider},
	}

	if _, err := provider.FetchStaples(t.Context(), "70100023"); err != nil {
		t.Fatalf("FetchStaples kroger returned error: %v", err)
	}
	if krogerBackend.calls != 1 || wholeFoodsProvider.calls != 0 {
		t.Fatalf("expected kroger provider to be selected, got kroger=%d wholefoods=%d", krogerBackend.calls, wholeFoodsProvider.calls)
	}

	if _, err := provider.FetchStaples(t.Context(), "wholefoods_10216"); err != nil {
		t.Fatalf("FetchStaples whole foods returned error: %v", err)
	}
	if krogerBackend.calls != 1 || wholeFoodsProvider.calls != 1 {
		t.Fatalf("expected whole foods provider to be selected once, got kroger=%d wholefoods=%d", krogerBackend.calls, wholeFoodsProvider.calls)
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

func TestRoutingStaplesProvider_FetchWines_SelectsProviderByLocationID(t *testing.T) {
	krogerBackend := &stubStaplesProvider{
		ids:         map[string]bool{"70100023": true},
		ingredients: []ai.InputIngredient{{ProductID: "1", Description: "Pinot Noir"}},
	}
	wholeFoodsProvider := &stubStaplesProvider{
		ids:         map[string]bool{"wholefoods_10216": true},
		ingredients: []ai.InputIngredient{{ProductID: "2", Description: "Whole Foods Red"}},
	}
	provider := routingStaplesProvider{
		backends: []backendStaplesProvider{krogerBackend, wholeFoodsProvider},
	}

	got, err := provider.FetchWines(t.Context(), "wholefoods_10216", []string{"Pinot Noir"})
	if err != nil {
		t.Fatalf("FetchWines returned error: %v", err)
	}
	if len(got) != 1 || got[0].Description != "Whole Foods Red" {
		t.Fatalf("unexpected wines: %+v", got)
	}
	if krogerBackend.calls != 0 || wholeFoodsProvider.calls != 1 {
		t.Fatalf("expected whole foods provider to be selected, got kroger=%d wholefoods=%d", krogerBackend.calls, wholeFoodsProvider.calls)
	}
}

func TestRoutingStaplesProvider_DoesNotDedupeIngredients(t *testing.T) {
	backend := &stubStaplesProvider{
		ids: map[string]bool{"70100023": true},
		ingredients: []ai.InputIngredient{
			{ProductID: "apple-1", Description: "Honeycrisp Apple"},
			{ProductID: "apple-1", Description: "Honeycrisp Apple"},
		},
	}
	provider := routingStaplesProvider{
		backends: []backendStaplesProvider{backend},
	}

	got, err := provider.FetchStaples(t.Context(), "70100023")
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected routing provider to preserve backend ingredients, got %+v", got)
	}
}

func TestDedupingStaplesProvider_FetchStaplesDedupesProductIDs(t *testing.T) {
	provider := dedupingStaplesProvider{
		provider: &stubRoutingStaplesProvider{
			ingredients: []ai.InputIngredient{
				{ProductID: "apple-1", Description: "Honeycrisp Apple"},
				{ProductID: "apple-1", Description: "Honeycrisp Apple"},
				{ProductID: "spinach-1", Description: "Baby Spinach"},
			},
		},
	}

	got, err := provider.FetchStaples(t.Context(), "70100023")
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected deduped ingredients, got %+v", got)
	}
	if got[0].ProductID != "apple-1" || got[1].ProductID != "spinach-1" {
		t.Fatalf("expected first occurrence of each product id in order, got %+v", got)
	}
}

func TestDedupingStaplesProvider_FetchWinesDedupesProductIDs(t *testing.T) {
	provider := dedupingStaplesProvider{
		provider: &stubRoutingStaplesProvider{
			ingredients: []ai.InputIngredient{
				{ProductID: "wine-1", Description: "Pinot Noir"},
				{ProductID: "wine-1", Description: "Pinot Noir"},
				{ProductID: "wine-2", Description: "Cabernet Sauvignon"},
			},
		},
	}

	got, err := provider.FetchWines(t.Context(), "70100023", []string{"pinot noir"})
	if err != nil {
		t.Fatalf("FetchWines returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected deduped wines, got %+v", got)
	}
	if got[0].ProductID != "wine-1" || got[1].ProductID != "wine-2" {
		t.Fatalf("expected first occurrence of each product id in order, got %+v", got)
	}
}

func TestDedupingStaplesProvider_RejectsBlankProductID(t *testing.T) {
	provider := dedupingStaplesProvider{
		provider: &stubRoutingStaplesProvider{
			ingredients: []ai.InputIngredient{{Description: "Mystery Ingredient"}},
		},
	}

	_, err := provider.FetchStaples(t.Context(), "70100023")
	if err == nil {
		t.Fatal("expected blank product id error")
	}
	if !strings.Contains(err.Error(), "blank product id for ingredient") {
		t.Fatalf("unexpected error: %v", err)
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

func TestFetchStaples_UsesProviderAndCachesWholeFoodsResults(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	provider := &stubStaplesProvider{
		ids: map[string]bool{"wholefoods_10216": true},
		ingredients: []ai.InputIngredient{
			{ProductID: "apple-1", Description: "Honeycrisp Apple"},
			{ProductID: "apple-1", Description: "Honeycrisp Apple"},
			{ProductID: "spinach-1", Description: "Baby Spinach"},
		},
	}
	s := &cachedStaplesService{
		cache:    IO(cacheStore),
		provider: provider,
		grader:   &stubIngredientGrader{},
	}

	params := &generatorParams{
		Location: &locations.Location{ID: "wholefoods_10216", Name: "Westlake"},
		Date:     time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
	}

	got, err := s.FetchStaples(t.Context(), params)
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider to be called once, got %d", provider.calls)
	}
	if got[0].ProductID == "" {
		t.Fatalf("expected input ingredient product id, got %+v", got)
	}

	gotAgain, err := s.FetchStaples(t.Context(), params)
	if err != nil {
		t.Fatalf("FetchStaples returned error on cached call: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected cached call to skip provider, got %d calls", provider.calls)
	}
	if len(gotAgain) != 3 {
		t.Fatalf("expected cached results, got %d", len(gotAgain))
	}
}

func TestFetchStaples_GradesCachedIngredientsBeforeReturning(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	grader := &stubIngredientGrader{}
	steak := ai.InputIngredient{ProductID: "steak-1", Description: "Ribeye Steak"}
	chips := ai.InputIngredient{ProductID: "chips-1", Description: "Potato Chips"}
	s := &cachedStaplesService{
		cache:  IO(cacheStore),
		grader: grader,
		provider: &stubRoutingStaplesProvider{
			ingredients: []ai.InputIngredient{chips, steak},
		},
	}

	params := &generatorParams{
		Location: &locations.Location{ID: "70100023", Name: "Test Store"},
		Date:     time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}

	got, err := s.FetchStaples(t.Context(), params)
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
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
