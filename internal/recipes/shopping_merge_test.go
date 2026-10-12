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

func (m *countingShoppingMerger) MergeShoppingQuantities(ctx context.Context, ingredients []ai.Ingredient) ([]ai.Ingredient, error) {
	m.calls++
	return (SimpleShoppingQuantityMerger{}).MergeShoppingQuantities(ctx, ingredients)
}

func TestMergedShoppingListCachedByFinalHash(t *testing.T) {
	s := newTestServer(t)
	merger := &countingShoppingMerger{}
	s.shoppingMerger = merger
	ingredients := []ai.Ingredient{{ProductID: "garlic-id", Name: "Garlic", Quantity: "1 cloves"}, {ProductID: "garlic-id", Name: "Garlic", Quantity: "2 cloves"}}
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
	ingredients[0].Price = "$3"
	updated, err := s.mergedShoppingList(t.Context(), "final-hash", ingredients)
	require.NoError(t, err)
	assert.Equal(t, 3, merger.calls)
	assert.Equal(t, "$3", updated[0].Items[0].Price)
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

type shoppingMergerFunc func(context.Context, []ai.Ingredient) ([]ai.Ingredient, error)

func (f shoppingMergerFunc) MergeShoppingQuantities(ctx context.Context, ingredients []ai.Ingredient) ([]ai.Ingredient, error) {
	return f(ctx, ingredients)
}

func TestShoppingQuantitiesRetriesMergeFailure(t *testing.T) {
	s := newTestServer(t)
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient) ([]ai.Ingredient, error) {
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
