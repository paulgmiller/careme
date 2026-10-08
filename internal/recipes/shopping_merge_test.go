package recipes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	s.locServer = staticLocationLookup{location: p.Location}
	require.NoError(t, s.SaveParams(t.Context(), p))
	req := httptest.NewRequest(http.MethodGet, "/recipes/"+p.Hash()+"/shopping-quantities", nil)
	req.SetPathValue("hash", p.Hash())
	rr := httptest.NewRecorder()
	s.handleShoppingQuantities(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Add to Kroger cart")
	assert.Contains(t, rr.Body.String(), "2 cloves")
}

func TestFinalizedKrogerIngredientsUsesProviderID(t *testing.T) {
	for _, tc := range []struct {
		id      string
		chain   string
		wantErr bool
	}{
		{id: "70500874", chain: "QFC"},
		{id: "01400943", chain: "KROGER"},
		{id: "70100123", chain: "FREDMEYER"},
		{id: "walmart_123", chain: "Kroger", wantErr: true},
	} {
		t.Run(tc.id+tc.chain, func(t *testing.T) {
			s := newTestServer(t)
			p := DefaultParams(&locations.Location{ID: tc.id, Chain: tc.chain}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
			s.locServer = staticLocationLookup{location: p.Location}
			require.NoError(t, s.SaveParams(t.Context(), p))
			ingredients, location, err := s.finalizedKrogerIngredients(t.Context(), p.Hash())
			if tc.wantErr {
				require.ErrorContains(t, err, "not a finalized Kroger list")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.chain, location.Chain)
			assert.Equal(t, p.Saved[0].Ingredients, ingredients)
		})
	}
}

func TestQFCTransferLinksToQFCCart(t *testing.T) {
	s := newTestServer(t)
	s.krogerCart = &kroger.CartClient{}
	p := DefaultParams(&locations.Location{ID: "70500874", Name: "Bellevue", Chain: "QFC"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
	s.locServer = staticLocationLookup{location: p.Location}
	require.NoError(t, s.SaveParams(t.Context(), p))
	require.NoError(t, s.saveCartTransfer(t.Context(), "mock-clerk-user-id", p.Hash(), krogerTransfer{Status: "complete", Added: 1, Sent: []ai.Ingredient{{Name: "Garlic"}}}, cache.Unconditional()))
	page := renderKrogerShoppingSection(t, s, p.Hash(), "")
	assert.Contains(t, page, `href="https://www.qfc.com/cart"`)
	assert.Contains(t, page, "Review QFC cart")
	assert.NotContains(t, page, "www.kroger.com/cart")
}

func TestLegacyQFCPlanUsesCurrentChainForCartAndAuthorization(t *testing.T) {
	s := newTestServer(t, withTestLocationServer(staticLocationLookup{location: &locations.Location{ID: "70500874", Name: "QFC Bellevue", Chain: "QFC"}}))
	s.krogerCart = &kroger.CartClient{ClientID: "client", RedirectURI: "https://careme.test/kroger/callback"}
	p := DefaultParams(&locations.Location{ID: "70500874", Name: "QFC Bellevue", Chain: "kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	page := renderKrogerShoppingSection(t, s, p.Hash(), "denied")
	assert.Contains(t, page, `href="https://www.qfc.com/cart"`)
	assert.Contains(t, page, "Review QFC cart")
	assert.NotContains(t, page, "www.kroger.com/cart")
	req := httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/kroger-cart", nil)
	req.SetPathValue("hash", p.Hash())
	rr := httptest.NewRecorder()
	s.handleKrogerCart(rr, req)
	require.Equal(t, http.StatusSeeOther, rr.Code)
	redirect, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "qfc", redirect.Query().Get("banner"))
}

type cartLocationLookupFunc func(context.Context, string) (*locations.Location, error)

func (f cartLocationLookupFunc) GetLocationByID(ctx context.Context, id string) (*locations.Location, error) {
	return f(ctx, id)
}

func TestKrogerCartRejectsFailedCurrentLocationLookup(t *testing.T) {
	lookupErr := errors.New("location API unavailable")
	s := newTestServer(t, withTestLocationServer(cartLocationLookupFunc(func(_ context.Context, id string) (*locations.Location, error) {
		assert.Equal(t, "70500874", id)
		return nil, lookupErr
	})))
	s.krogerCart = &kroger.CartClient{}
	p := DefaultParams(&locations.Location{ID: "70500874", Chain: "kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	_, _, err := s.finalizedKrogerIngredients(t.Context(), p.Hash())
	require.ErrorIs(t, err, lookupErr)
	require.ErrorContains(t, err, "load current Kroger location")
	for _, handler := range []http.HandlerFunc{s.handleKrogerCart, s.handleShoppingQuantities} {
		req := httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/kroger-cart", nil)
		req.SetPathValue("hash", p.Hash())
		rr := httptest.NewRecorder()
		handler(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assert.Empty(t, rr.Header().Get("Location"))
		assert.NotContains(t, rr.Body.String(), "www.kroger.com/cart")
	}
}

func TestKrogerCartLink(t *testing.T) {
	for _, tc := range []struct {
		name  string
		url   string
		brand string
	}{
		{name: "QFC", url: "https://www.qfc.com/cart", brand: "QFC"},
		{name: "qfc", url: "https://www.qfc.com/cart", brand: "QFC"},
		{name: "KROGER", url: "https://www.kroger.com/cart", brand: "Kroger"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, brand := krogerCartLink(tc.name)
			assert.Equal(t, tc.url, url)
			assert.Equal(t, tc.brand, brand)
		})
	}
}
