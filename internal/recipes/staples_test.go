package recipes

import (
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/wholefoods"
	"context"
	"slices"
	"testing"
	"time"
)

type stubStaplesProvider struct {
	ingredients []kroger.Ingredient
	err         error
	calls       int
}

func (s *stubStaplesProvider) FetchStaples(_ context.Context, _ *locations.Location, _ []filter) ([]kroger.Ingredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

type stubWholeFoodsClient struct {
	results map[string][]wholefoods.Product
	errs    map[string]error
	calls   []string
}

func (s *stubWholeFoodsClient) Category(_ context.Context, queryterm, store string) (*wholefoods.CategoryResponse, error) {
	s.calls = append(s.calls, store+":"+queryterm)
	if err := s.errs[queryterm]; err != nil {
		return nil, err
	}
	return &wholefoods.CategoryResponse{Results: slices.Clone(s.results[queryterm])}, nil
}

func TestRoutingStaplesProvider_SelectsProviderByLocationID(t *testing.T) {
	krogerProvider := &stubStaplesProvider{}
	wholeFoodsProvider := &stubStaplesProvider{}
	provider := routingStaplesProvider{
		kroger:     krogerProvider,
		wholeFoods: wholeFoodsProvider,
	}

	if _, err := provider.FetchStaples(t.Context(), &locations.Location{ID: "70100023"}, []filter{{Term: "fresh produce"}}); err != nil {
		t.Fatalf("FetchStaples kroger returned error: %v", err)
	}
	if krogerProvider.calls != 1 || wholeFoodsProvider.calls != 0 {
		t.Fatalf("expected kroger provider to be selected, got kroger=%d wholefoods=%d", krogerProvider.calls, wholeFoodsProvider.calls)
	}

	if _, err := provider.FetchStaples(t.Context(), &locations.Location{ID: "wholefoods_10216"}, []filter{{Term: "vegetables"}}); err != nil {
		t.Fatalf("FetchStaples whole foods returned error: %v", err)
	}
	if krogerProvider.calls != 1 || wholeFoodsProvider.calls != 1 {
		t.Fatalf("expected whole foods provider to be selected once, got kroger=%d wholefoods=%d", krogerProvider.calls, wholeFoodsProvider.calls)
	}
}

func TestRoutingStaplesProvider_RejectsUnsupportedLocationBackend(t *testing.T) {
	provider := routingStaplesProvider{
		kroger:     &stubStaplesProvider{},
		wholeFoods: &stubStaplesProvider{},
	}

	_, err := provider.FetchStaples(t.Context(), &locations.Location{ID: "walmart_3098"}, []filter{{Term: "produce"}})
	if err == nil {
		t.Fatal("expected unsupported backend error")
	}
	if got, want := err.Error(), `staples provider does not support location "walmart_3098"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestWholeFoodsStaplesProvider_MapsProductsToIngredients(t *testing.T) {
	client := &stubWholeFoodsClient{
		results: map[string][]wholefoods.Product{
			"vegetables": {
				{
					Name:         "Organic Asparagus",
					Slug:         "organic-asparagus",
					Brand:        "Whole Foods Market",
					Store:        10216,
					UOM:          "1 lb",
					RegularPrice: 5.99,
					SalePrice:    4.49,
				},
			},
		},
	}
	provider := wholeFoodsStaplesProvider{client: client}

	got, err := provider.FetchStaples(t.Context(), &locations.Location{ID: "wholefoods_10216"}, []filter{{Term: "vegetables"}})
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 ingredient, got %d", len(got))
	}

	ingredient := got[0]
	if ingredient.Description == nil || *ingredient.Description != "Organic Asparagus" {
		t.Fatalf("unexpected description: %+v", ingredient.Description)
	}
	if ingredient.Brand == nil || *ingredient.Brand != "Whole Foods Market" {
		t.Fatalf("unexpected brand: %+v", ingredient.Brand)
	}
	if ingredient.Size == nil || *ingredient.Size != "1 lb" {
		t.Fatalf("unexpected size: %+v", ingredient.Size)
	}
	if ingredient.ProductId == nil || *ingredient.ProductId != "10216:organic-asparagus" {
		t.Fatalf("unexpected product id: %+v", ingredient.ProductId)
	}
	if ingredient.PriceRegular == nil || *ingredient.PriceRegular != float32(5.99) {
		t.Fatalf("unexpected regular price: %+v", ingredient.PriceRegular)
	}
	if ingredient.PriceSale == nil || *ingredient.PriceSale != float32(4.49) {
		t.Fatalf("unexpected sale price: %+v", ingredient.PriceSale)
	}
}

func TestGetStaples_UsesProviderAndCachesWholeFoodsResults(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	provider := &stubStaplesProvider{
		ingredients: []kroger.Ingredient{
			{Description: loPtr("Honeycrisp Apple")},
			{Description: loPtr("Honeycrisp Apple")},
			{Description: loPtr("Baby Spinach")},
		},
	}
	g := &Generator{
		io:              IO(cacheStore),
		staplesProvider: provider,
	}
	params := &generatorParams{
		Location: &locations.Location{ID: "wholefoods_10216", Name: "Westlake"},
		Date:     time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
		Staples:  WholeFoodsStaples(),
	}

	got, err := g.GetStaples(t.Context(), params)
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

	gotAgain, err := g.GetStaples(t.Context(), params)
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

func TestWholeFoodsStaplesProvider_InvalidLocationID(t *testing.T) {
	provider := wholeFoodsStaplesProvider{client: &stubWholeFoodsClient{}}

	_, err := provider.FetchStaples(t.Context(), &locations.Location{ID: "10216"}, []filter{{Term: "vegetables"}})
	if err == nil {
		t.Fatalf("expected invalid location error, got %v", err)
	}
	if got, want := err.Error(), `invalid whole foods location id "10216"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}
