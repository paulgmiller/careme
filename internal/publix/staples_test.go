package publix

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sync"
	"testing"
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

type stubSavingsClient struct {
	results map[string]StoreProductsSavingsResult
	mu      sync.Mutex
	calls   []StoreProductsSavingsOptions
}

func (s *stubSavingsClient) StoreProductsSavings(_ context.Context, opts StoreProductsSavingsOptions) (*StoreProductsSavingsResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, opts)
	s.mu.Unlock()

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
	return slices.ContainsFunc(s.calls, func(call StoreProductsSavingsOptions) bool {
		return call.StoreNumber == store &&
			call.CategoryID == category &&
			call.Abck == abck &&
			call.Take == take &&
			call.Skip == skip
	})
}

func TestStaplesProvider_MapsProductsToIngredients(t *testing.T) {
	t.Parallel()

	priceLine := "2 for $5.00"
	sizeDescription := "1 lb"
	client := &stubSavingsClient{
		results: map[string]StoreProductsSavingsResult{
			CategoryVegetables: {
				StoreProducts: []StoreProduct{{
					ItemCode:        101,
					Title:           "Publix Asparagus",
					PriceLine:       &priceLine,
					SizeDescription: &sizeDescription,
				}},
				TotalCount: 1,
			},
		},
	}
	provider := newStaplesProvider(client, "akamai-token")

	got, err := provider.FetchStaples(t.Context(), "publix_1847")
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if got, want := client.callCount(), len(StapleCategories()); got != want {
		t.Fatalf("expected %d category calls, got %d", want, got)
	}
	if !client.hasCall("1847", CategoryVegetables, "akamai-token", defaultStapleTake, 0) {
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
		ItemCode:        96320,
		Title:           "Publix Veal Cubed Steaks, USDA Choice, Group Raised",
		PriceLine:       nil,
		SizeDescription: nil,
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
	provider := newStaplesProvider(client, "akamai-token")

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

func TestStaplesProvider_InvalidLocationID(t *testing.T) {
	t.Parallel()

	provider := newStaplesProvider(&stubSavingsClient{}, "akamai-token")
	_, err := provider.FetchStaples(t.Context(), "1847")
	if err == nil {
		t.Fatal("expected invalid location error")
	}
	if got, want := err.Error(), `invalid publix location id "1847"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}
