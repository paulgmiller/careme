package recipes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/providers/kroger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingShoppingMerger struct{ calls int }

func (m *countingShoppingMerger) MergeShoppingQuantities(_ context.Context, groups []ai.ShoppingQuantityGroup) (map[string]string, error) {
	m.calls++
	result := make(map[string]string)
	for _, group := range groups {
		result[group.Key] = "3 cloves"
	}
	return result, nil
}

func TestMergedShoppingListCachedByFinalHash(t *testing.T) {
	s := newTestServer(t)
	merger := &countingShoppingMerger{}
	s.shoppingMerger = merger
	ingredients := []ai.Ingredient{{ProductID: "garlic-id", Name: "Garlic", Quantity: "1 clove"}, {ProductID: "garlic-id", Name: "Garlic", Quantity: "2 cloves"}}
	first, err := s.mergedShoppingList(t.Context(), "final-hash", ingredients)
	require.NoError(t, err)
	second, err := s.mergedShoppingList(t.Context(), "final-hash", ingredients)
	require.NoError(t, err)
	assert.Equal(t, 1, merger.calls)
	assert.Equal(t, "3 cloves", first[0].Items[0].Quantity)
	assert.Equal(t, first, second)
	_, err = s.mergedShoppingList(t.Context(), "final-hash", append(ingredients, ai.Ingredient{ProductID: "wine-id", Name: "Wine", Quantity: "1 bottle"}))
	require.NoError(t, err)
	assert.Equal(t, 2, merger.calls)
}

func TestFinalizedKrogerShoppingSectionShowsCartAction(t *testing.T) {
	s := newTestServer(t)
	s.krogerCart = &kroger.CartClient{}
	p := DefaultParams(&locations.Location{ID: "01400943", Chain: "Kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "0001111060903", Name: "Garlic", Quantity: "2 cloves"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	req := httptest.NewRequest(http.MethodGet, "/recipes/"+p.Hash()+"/shopping-quantities", nil)
	req.SetPathValue("hash", p.Hash())
	rr := httptest.NewRecorder()
	s.handleShoppingQuantities(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Add to Kroger cart")
	assert.Contains(t, rr.Body.String(), "2 cloves")
}

func TestQFCTransferLinksToQFCCart(t *testing.T) {
	s := newTestServer(t)
	s.krogerCart = &kroger.CartClient{}
	p := DefaultParams(&locations.Location{ID: "70500874", Name: "QFC Bellevue", Chain: "kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	require.NoError(t, s.saveCartTransfer(t.Context(), "mock-clerk-user-id", p.Hash(), krogerTransfer{Status: "complete", Added: 1, Sent: []ai.Ingredient{{Name: "Garlic"}}}, cache.Unconditional()))
	page := renderKrogerShoppingSection(t, s, p.Hash(), "")
	assert.Contains(t, page, `href="https://www.qfc.com/cart"`)
	assert.Contains(t, page, "Review QFC cart")
	assert.NotContains(t, page, "www.kroger.com/cart")
}

func TestKrogerCartLink(t *testing.T) {
	for _, tc := range []struct {
		name  string
		url   string
		brand string
	}{
		{name: "QFC", url: "https://www.qfc.com/cart", brand: "QFC"},
		{name: " QfC Bellevue ", url: "https://www.qfc.com/cart", brand: "QFC"},
		{name: "QFC-Bellevue", url: "https://www.qfc.com/cart", brand: "QFC"},
		{name: "Kroger on the Rhine", url: "https://www.kroger.com/cart", brand: "Kroger"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, brand := krogerCartLink(tc.name)
			assert.Equal(t, tc.url, url)
			assert.Equal(t, tc.brand, brand)
		})
	}
}
