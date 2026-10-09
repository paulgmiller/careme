package recipes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"

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

func TestFinalizedShoppingSection(t *testing.T) {
	for _, id := range []string{"01400943", "walmart_123", "mnfoodclub_delivery"} {
		t.Run(id, func(t *testing.T) {
			s := newTestServer(t)
			p := DefaultParams(&locations.Location{ID: id}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "garlic", Name: "Garlic", Quantity: "2 cloves"}, {ProductID: "garlic", Name: "Garlic", Quantity: "1 cloves"}}}}
			require.NoError(t, s.SaveParams(t.Context(), p))
			req := httptest.NewRequest(http.MethodGet, "/recipes/"+p.Hash()+"/shopping-quantities", nil)
			req.SetPathValue("hash", p.Hash())
			rr := httptest.NewRecorder()
			s.handleShoppingQuantities(rr, req)
			require.Equal(t, http.StatusOK, rr.Code)
			assert.Contains(t, rr.Body.String(), "3 cloves")
			assert.NotContains(t, rr.Body.String(), "cart")
			isValidHTML(t, rr.Body.String())
		})
	}
}

type shoppingMergerFunc func(context.Context, []ai.ShoppingQuantityGroup) (map[string]string, error)

func (f shoppingMergerFunc) MergeShoppingQuantities(ctx context.Context, groups []ai.ShoppingQuantityGroup) (map[string]string, error) {
	return f(ctx, groups)
}

func TestShoppingQuantitiesRetriesMergeFailure(t *testing.T) {
	s := newTestServer(t)
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.ShoppingQuantityGroup) (map[string]string, error) {
		return nil, errors.New("AI unavailable")
	})
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "1 clove"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	req := httptest.NewRequest(http.MethodGet, "/recipes/"+p.Hash()+"/shopping-quantities", nil)
	req.SetPathValue("hash", p.Hash())
	rr := httptest.NewRecorder()
	s.handleShoppingQuantities(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Try again, chef")
	assert.Contains(t, rr.Body.String(), `/recipes/`+p.Hash()+`/shopping-quantities`)
	assert.NotContains(t, rr.Body.String(), "1 clove")
}

func TestShoppingQuantitiesRejectsUnfinalizedList(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "walmart_123"}, time.Now())
	require.NoError(t, s.SaveParams(t.Context(), p))
	req := httptest.NewRequest(http.MethodGet, "/recipes/"+p.Hash()+"/shopping-quantities", nil)
	req.SetPathValue("hash", p.Hash())
	rr := httptest.NewRecorder()
	s.handleShoppingQuantities(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestMergedShoppingListRequiresEveryQuantity(t *testing.T) {
	s := newTestServer(t)
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.ShoppingQuantityGroup) (map[string]string, error) {
		return map[string]string{}, nil
	})
	_, err := s.mergedShoppingList(t.Context(), "missing", []ai.Ingredient{{Name: "Garlic", Quantity: "1 clove"}})
	require.ErrorContains(t, err, "missing merged shopping quantity")
}
