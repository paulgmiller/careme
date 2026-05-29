package publix

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/cache"
)

func TestIdentityProviderSignature_UsesStapleCategories(t *testing.T) {
	t.Parallel()

	got := NewIdentityProvider().Signature()
	want, err := json.Marshal(StapleCategories())
	if err != nil {
		t.Fatalf("marshal staple categories: %v", err)
	}
	if got != string(want) {
		t.Fatalf("unexpected signature: got %q want %q", got, want)
	}
}

func TestStapleCategories_IncludesKnownPublixStaples(t *testing.T) {
	t.Parallel()

	got := map[string]string{}
	for _, category := range StapleCategories() {
		got[category.Name] = category.ID
	}

	want := map[string]string{
		"vegetables":      CategoryVegetables,
		"fruit":           CategoryFruit,
		"beef":            CategoryBeef,
		"veal":            CategoryVeal,
		"chicken":         CategoryChicken,
		"lamb":            CategoryLamb,
		"sausage":         CategorySausage,
		"fish":            CategoryFish,
		"scallops":        CategoryScallops,
		"pasta":           CategoryPasta,
		"rice and grains": CategoryRiceGrains,
	}

	if !maps.Equal(got, want) {
		t.Fatalf("unexpected staple categories: got %+v want %+v", got, want)
	}
}

func TestStapleCategories_ProduceTakesTwoHundredFifty(t *testing.T) {
	t.Parallel()

	got := map[string]int{}
	for _, category := range StapleCategories() {
		got[category.Name] = category.Take
	}

	if got["vegetables"] != produceStapleTake {
		t.Fatalf("unexpected vegetables take: got %d want %d", got["vegetables"], produceStapleTake)
	}
	if got["fruit"] != produceStapleTake {
		t.Fatalf("unexpected fruit take: got %d want %d", got["fruit"], produceStapleTake)
	}
}

type stubSavingsClient struct {
	results   map[string]StoreProductsSavingsResult
	resultFor func(StoreProductsSavingsOptions) StoreProductsSavingsResult
	mu        sync.Mutex
	calls     []StoreProductsSavingsOptions
}

func (s *stubSavingsClient) StoreProductsSavings(_ context.Context, opts StoreProductsSavingsOptions) (*StoreProductsSavingsResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, opts)
	s.mu.Unlock()

	if s.resultFor != nil {
		result := s.resultFor(opts)
		return &result, nil
	}
	result := s.results[opts.CategoryID]
	return &result, nil
}

func (s *stubSavingsClient) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *stubSavingsClient) hasCall(store, category, abck string, take, skip int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	hasCall := slices.ContainsFunc(s.calls, func(call StoreProductsSavingsOptions) bool {
		return call.StoreNumber == store &&
			call.CategoryID == category &&
			call.Abck == abck &&
			call.Take == take &&
			call.Skip == skip
	})
	if hasCall {
		return true
	}

	fmt.Printf("missing savings call: want store=%q category=%q abck=%q take=%d skip=%d\n", store, category, abck, take, skip)
	if len(s.calls) == 0 {
		fmt.Println("got no savings calls")
		return false
	}
	for i, call := range s.calls {
		fmt.Printf("got savings call %d: %s\n", i, savingsCallDiff(call, store, category, abck, take, skip))
	}
	return false
}

func (s *stubSavingsClient) categoryCallCount(category string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int
	for _, call := range s.calls {
		if call.CategoryID == category {
			count++
		}
	}
	return count
}

func savingsCallDiff(call StoreProductsSavingsOptions, store, category, abck string, take, skip int) string {
	diffs := []string{}
	if call.StoreNumber != store {
		diffs = append(diffs, fmt.Sprintf("store got %q want %q", call.StoreNumber, store))
	}
	if call.CategoryID != category {
		diffs = append(diffs, fmt.Sprintf("category got %q want %q", call.CategoryID, category))
	}
	if call.Abck != abck {
		diffs = append(diffs, fmt.Sprintf("abck got %q want %q", call.Abck, abck))
	}
	if call.Take != take {
		diffs = append(diffs, fmt.Sprintf("take got %d want %d", call.Take, take))
	}
	if call.Skip != skip {
		diffs = append(diffs, fmt.Sprintf("skip got %d want %d", call.Skip, skip))
	}
	if len(diffs) == 0 {
		return "matches"
	}
	return strings.Join(diffs, ", ")
}

func TestStaplesProvider_PaginatesProduceStaples(t *testing.T) {
	t.Parallel()

	client := &stubSavingsClient{
		resultFor: func(opts StoreProductsSavingsOptions) StoreProductsSavingsResult {
			if opts.CategoryID != CategoryVegetables && opts.CategoryID != CategoryFruit {
				return StoreProductsSavingsResult{}
			}

			products := make([]StoreProduct, opts.Take)
			for i := range products {
				products[i] = StoreProduct{
					ItemCode: opts.Skip + i + 1,
					Title:    fmt.Sprintf("produce item %d", opts.Skip+i+1),
				}
			}
			return StoreProductsSavingsResult{
				StoreProducts: products,
				TotalCount:    300,
			}
		},
	}
	provider := newStaplesProviderWithCache(client, defaultLoadAbck)

	got, err := provider.FetchStaples(t.Context(), "publix_1847")
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}

	if got, want := len(got), produceStapleTake*2; got != want {
		t.Fatalf("unexpected ingredient count: got %d want %d", got, want)
	}
	for _, category := range []string{CategoryVegetables, CategoryFruit} {
		if !client.hasCall("1847", category, "akamai-token", bigStapleTake, 0) {
			t.Fatalf("missing first produce page call for %s", category)
		}
		if !client.hasCall("1847", category, "akamai-token", bigStapleTake, 100) {
			t.Fatalf("missing second produce page call for %s", category)
		}
		if !client.hasCall("1847", category, "akamai-token", 50, 200) {
			t.Fatalf("missing final capped produce page call for %s", category)
		}
		if got, want := client.categoryCallCount(category), 3; got != want {
			t.Fatalf("unexpected produce page count for %s: got %d want %d", category, got, want)
		}
	}
}

func TestStaplesProvider_MapsProductsToIngredients(t *testing.T) {
	t.Parallel()

	priceLine := "2 for $5.00"
	originalPriceLine := "$3.49"
	sizeDescription := "1 lb"
	client := &stubSavingsClient{
		results: map[string]StoreProductsSavingsResult{
			CategoryVegetables: {
				StoreProducts: []StoreProduct{{
					ItemCode:          101,
					Title:             "Publix Asparagus",
					PriceLine:         &priceLine,
					OriginalPriceLine: &originalPriceLine,
					SizeDescription:   &sizeDescription,
				}},
				TotalCount: 1,
			},
		},
	}
	provider := newStaplesProviderWithCache(client, defaultLoadAbck)

	got, err := provider.FetchStaples(t.Context(), "publix_1847")
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if got, want := client.callCount(), len(StapleCategories()); got != want {
		t.Fatalf("expected %d category calls, got %d", want, got)
	}
	if !client.hasCall("1847", CategoryVegetables, "akamai-token", bigStapleTake, 0) {
		t.Fatalf("missing vegetables category call")
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 ingredient, got %d", len(got))
	}

	ingredient := got[0]
	if ingredient.ProductID != "101" {
		t.Fatalf("unexpected product id: %q", ingredient.ProductID)
	}
	if ingredient.Description != "Publix Asparagus" {
		t.Fatalf("unexpected description: %q", ingredient.Description)
	}
	if ingredient.Size != "1 lb" {
		t.Fatalf("unexpected size: %q", ingredient.Size)
	}
	if ingredient.PriceSale == nil || *ingredient.PriceSale != float32(2.5) {
		t.Fatalf("unexpected sale price: %+v", ingredient.PriceSale)
	}
	if ingredient.PriceRegular == nil || *ingredient.PriceRegular != float32(3.49) {
		t.Fatalf("unexpected regular price: %+v", ingredient.PriceRegular)
	}
	if ingredient.AisleNumber != "vegetables" {
		t.Fatalf("unexpected aisle: %q", ingredient.AisleNumber)
	}
	if !slices.Equal(ingredient.Categories, []string{"vegetables"}) {
		t.Fatalf("unexpected categories: %+v", ingredient.Categories)
	}
}

func TestStaplesProvider_MapsNullPriceAndSize(t *testing.T) {
	t.Parallel()

	ingredient := productToIngredient(StoreProduct{
		ItemCode:          96320,
		Title:             "Publix Veal Cubed Steaks, USDA Choice, Group Raised",
		PriceLine:         nil,
		OriginalPriceLine: nil,
		SizeDescription:   nil,
	}, StapleCategory{Name: "beef", ID: CategoryBeef})

	if ingredient.ProductID != "96320" {
		t.Fatalf("unexpected product id: %q", ingredient.ProductID)
	}
	if ingredient.Description != "Publix Veal Cubed Steaks, USDA Choice, Group Raised" {
		t.Fatalf("unexpected description: %q", ingredient.Description)
	}
	if ingredient.Size != "" {
		t.Fatalf("unexpected size: %q", ingredient.Size)
	}
	if ingredient.PriceSale != nil {
		t.Fatalf("unexpected sale price: %+v", ingredient.PriceSale)
	}
	if ingredient.PriceRegular != nil {
		t.Fatalf("unexpected regular price: %+v", ingredient.PriceRegular)
	}
}

func TestStaplesProvider_GetIngredients_UsesCategoryAndSkip(t *testing.T) {
	t.Parallel()

	client := &stubSavingsClient{
		results: map[string]StoreProductsSavingsResult{
			CategoryBeef: {
				StoreProducts: []StoreProduct{{ItemCode: 96320, Title: "Publix Veal Cubed Steaks"}},
				TotalCount:    1,
			},
		},
	}

	provider := newStaplesProviderWithCache(client, defaultLoadAbck)

	got, err := provider.GetIngredients(t.Context(), "publix_1847", "beef", 48)
	if err != nil {
		t.Fatalf("GetIngredients returned error: %v", err)
	}
	if !client.hasCall("1847", CategoryBeef, "akamai-token", defaultStapleTake, 48) {
		t.Fatalf("missing beef category call")
	}
	if len(got) != 1 || got[0].Description != "Publix Veal Cubed Steaks" {
		t.Fatalf("unexpected ingredients: %+v", got)
	}
}

func TestStaplesProvider_UsesCachedAbck(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	err := SaveAbckRecord(t.Context(), cacheStore, AbckRecord{
		Cookie:    "cached-token",
		FetchedAt: time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC),
		SourceURL: "https://www.publix.com/c/beef/163c7c04-5495-404e-81fc-34f71b241093",
		Provider:  brightDataBrowserSource,
	})
	if err != nil {
		t.Fatalf("SaveAbckRecord returned error: %v", err)
	}

	client := &stubSavingsClient{
		results: map[string]StoreProductsSavingsResult{
			CategoryBeef: {
				StoreProducts: []StoreProduct{{ItemCode: 96320, Title: "Publix Veal Cubed Steaks"}},
				TotalCount:    1,
			},
		},
	}
	provider := newStaplesProviderWithCache(client, func(ctx context.Context) (string, error) {
		return cookieFromCache(ctx, cacheStore)
	})

	_, err = provider.GetIngredients(t.Context(), "publix_1847", "beef", 0)
	if err != nil {
		t.Fatalf("GetIngredients returned error: %v", err)
	}
	if !client.hasCall("1847", CategoryBeef, "cached-token", defaultStapleTake, 0) {
		t.Fatalf("missing beef category call with cached abck token")
	}
}

func TestStaplesProvider_InvalidLocationID(t *testing.T) {
	t.Parallel()

	provider := newStaplesProviderWithCache(&stubSavingsClient{}, defaultLoadAbck)
	_, err := provider.FetchStaples(t.Context(), "1847")
	if err == nil {
		t.Fatal("expected invalid location error")
	}
	if got, want := err.Error(), `invalid publix location id "1847"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func defaultLoadAbck(context.Context) (string, error) { return "akamai-token", nil }
