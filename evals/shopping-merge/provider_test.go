package eval

import (
	"context"
	"errors"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeMerger struct {
	needs   []ai.Ingredient
	catalog []ai.InputIngredient
	err     error
}

func (m *fakeMerger) MergeShoppingQuantities(_ context.Context, needs []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.ShoppingItem, error) {
	m.needs, m.catalog = needs, catalog
	return []ai.ShoppingItem{{Name: "Onion", Needed: "8 ounces"}}, m.err
}

func TestRunEval(t *testing.T) {
	ctx := map[string]any{"vars": map[string]any{"catalog": []ai.InputIngredient{{ProductID: "onion", Size: "1 lb"}}, "ingredients": []ai.Ingredient{{Name: "Jumbo Yellow Onion", Quantity: "4 ounces"}, {Name: "Jumbo Yellow Onions", Quantity: "4 ounces, about ½ medium onion"}}}}
	m := &fakeMerger{}
	result, err := runEval(ctx, m)
	require.NoError(t, err)
	require.Len(t, m.needs, 2)
	assert.Equal(t, "4 ounces, about ½ medium onion", m.needs[1].Quantity)
	assert.Empty(t, m.catalog)
	assert.JSONEq(t, `[{"id":"","name":"Onion","needed":"8 ounces","buy":null}]`, result["output"].(string))
	m.err = errors.New("API unavailable")
	result, err = runEval(ctx, m)
	require.ErrorContains(t, err, "merge shopping ingredients: API unavailable")
	assert.Nil(t, result)
}

func TestRunEvalRequiresIngredients(t *testing.T) {
	_, err := runEval(map[string]any{}, &fakeMerger{})
	require.ErrorContains(t, err, "ingredients are required")
}
