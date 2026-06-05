package heb

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
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

type stubHEBQueryClient struct {
	mu      sync.Mutex
	results map[string][]Product
	calls   []CategoryOptions
}

func (s *stubHEBQueryClient) Category(_ context.Context, opts CategoryOptions) ([]Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = append(s.calls, opts)
	key := opts.ParentID + ":" + opts.ChildID
	return s.results[key], nil
}

func (s *stubHEBQueryClient) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *stubHEBQueryClient) hasCall(want CategoryOptions) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.ContainsFunc(s.calls, func(got CategoryOptions) bool {
		return got.Reese84 == want.Reese84 &&
			got.StoreID == want.StoreID &&
			got.ParentID == want.ParentID &&
			got.ChildID == want.ChildID &&
			got.CategoryPath == want.CategoryPath &&
			got.Int == want.Int &&
			got.Limit == want.Limit
	})
}

func TestStapleCategories_HaveLimits(t *testing.T) {
	t.Parallel()

	got := make(map[string]int)
	for _, category := range StapleCategories() {
		got[category.Name] = category.Limit
	}

	tests := map[string]int{
		"vegetables": produceStapleLimit,
		"fruit":      produceStapleLimit,
		"beef":       bigStapleLimit,
		"pork":       bigStapleLimit,
		"chicken":    bigStapleLimit,
		"sausage":    defaultStapleLimit,
		"fish":       seafoodStapleLimit,
		"shrimp":     seafoodStapleLimit,
	}
	for name, want := range tests {
		if got[name] != want {
			t.Fatalf("unexpected %s limit: got %d want %d", name, got[name], want)
		}
	}
}

func TestStaplesProvider_MapsProductsToIngredients(t *testing.T) {
	t.Parallel()

	listPrice := float32(8.99)
	salePrice := float32(6.99)
	client := &stubHEBQueryClient{
		results: map[string][]Product{
			CategoryPorkParent + ":" + CategoryPorkChild: {
				{
					ID:                    "pork-1",
					DisplayName:           "H-E-B Pork Shoulder Roast",
					FullCategoryHierarchy: "Meat & seafood/Meat/Pork",
					Brand:                 &Brand{Name: "H-E-B"},
					ProductLocation:       &ProductLocation{Location: "Meat Market"},
					ListPrice:             &listPrice,
					SalePrice:             &salePrice,
				},
			},
		},
	}
	provider := newStaplesProviderWithClient(client, func(context.Context) (string, error) {
		return "cached-reese84", nil
	})

	got, err := provider.FetchStaples(t.Context(), "heb_92")
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if got, want := client.callCount(), len(StapleCategories()); got != want {
		t.Fatalf("unexpected call count: got %d want %d", got, want)
	}
	if !client.hasCall(CategoryOptions{
		Reese84:      "cached-reese84",
		StoreID:      "92",
		ParentID:     CategoryPorkParent,
		ChildID:      CategoryPorkChild,
		CategoryPath: "/category/shop/meat-seafood/meat/pork/490110/490536?int=curbside-category-shortcuts.meat.pork",
		Int:          "curbside-category-shortcuts.meat.pork",
		Limit:        bigStapleLimit,
	}) {
		t.Fatalf("missing pork category call")
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 ingredient, got %d", len(got))
	}

	assertInputIngredient(t, got[0], ai.InputIngredient{
		ProductID:    "pork-1",
		Description:  "H-E-B Pork Shoulder Roast",
		Brand:        "H-E-B",
		AisleNumber:  "Meat Market",
		Categories:   []string{"Meat & seafood", "Meat", "Pork"},
		PriceRegular: &listPrice,
		PriceSale:    &salePrice,
	})
}

func TestNewStaplesProvider_LoadsHEBCachedReese84(t *testing.T) {
	unsetEnvForTest(t, "AZURE_STORAGE_ACCOUNT_NAME")
	unsetEnvForTest(t, "AZURE_STORAGE_PRIMARY_ACCOUNT_KEY")
	t.Setenv(brightDataBrowserWSEnv, "wss://user:pass@example.com")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	hebCache, err := cache.EnsureCache(Container)
	if err != nil {
		t.Fatalf("EnsureCache returned error: %v", err)
	}
	if err := SaveReese84Record(t.Context(), hebCache, Reese84Record{
		Cookie:    "cached-heb-reese84",
		FetchedAt: time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC),
		Provider:  "test",
	}); err != nil {
		t.Fatalf("SaveReese84Record returned error: %v", err)
	}
	if err := saveLatestBuildID(t.Context(), hebCache, "cached-build"); err != nil {
		t.Fatalf("SaveLatestBuildID returned error: %v", err)
	}

	provider, err := NewStaplesProvider(nil)
	if err != nil {
		t.Fatalf("NewStaplesProvider returned error: %v", err)
	}
	queryClient, ok := provider.client.(*QueryClient)
	if !ok {
		t.Fatalf("expected *QueryClient, got %T", provider.client)
	}
	if queryClient.buildID != "cached-build" {
		t.Fatalf("unexpected initial build id: %q", queryClient.buildID)
	}
	got, err := provider.loadReese84(t.Context())
	if err != nil {
		t.Fatalf("loadReese84 returned error: %v", err)
	}
	if got != "cached-heb-reese84" {
		t.Fatalf("unexpected reese84 token: %q", got)
	}
}

func TestStaplesProvider_InvalidLocationID(t *testing.T) {
	t.Parallel()

	provider := newStaplesProviderWithClient(&stubHEBQueryClient{}, func(context.Context) (string, error) {
		t.Fatal("unexpected reese84 load")
		return "", nil
	})

	_, err := provider.FetchStaples(t.Context(), "92")
	if err == nil || err.Error() != `invalid heb location id "92"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStaplesProvider_ReturnsReese84LoadError(t *testing.T) {
	t.Parallel()

	provider := newStaplesProviderWithClient(&stubHEBQueryClient{}, func(context.Context) (string, error) {
		return "", errors.New("missing token")
	})

	_, err := provider.FetchStaples(t.Context(), "heb_92")
	if err == nil || !strings.Contains(err.Error(), "missing token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStaplesProvider_FetchWinesUnsupported(t *testing.T) {
	t.Parallel()

	provider := newStaplesProviderWithClient(&stubHEBQueryClient{}, func(context.Context) (string, error) {
		return "cached-reese84", nil
	})

	_, err := provider.FetchWines(t.Context(), "heb_92", []string{"Pinot Noir"})
	if err == nil || err.Error() != `wine lookup is not supported for location "heb_92"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertInputIngredient(t *testing.T, got, want ai.InputIngredient) {
	t.Helper()
	if got.ProductID != want.ProductID ||
		got.Description != want.Description ||
		got.Brand != want.Brand ||
		got.AisleNumber != want.AisleNumber ||
		!sameFloat32Ptr(got.PriceRegular, want.PriceRegular) ||
		!sameFloat32Ptr(got.PriceSale, want.PriceSale) ||
		!slices.Equal(got.Categories, want.Categories) {
		t.Fatalf("unexpected ingredient: got %+v want %+v", got, want)
	}
}

func sameFloat32Ptr(left, right *float32) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func unsetEnvForTest(t *testing.T, name string) {
	t.Helper()

	old, ok := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("Unsetenv(%q) returned error: %v", name, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(name, old)
			return
		}
		_ = os.Unsetenv(name)
	})
}
