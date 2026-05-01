package recipes

import (
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
)

func TestShoppingListForDisplay(t *testing.T) {
	tests := []struct {
		name        string
		ingredients []ai.Ingredient
		want        []*ai.Ingredient
	}{
		{
			name: "empty list returns empty result",
			want: nil,
		},
		{
			name: "combines quantities and preserves first-seen order",
			ingredients: []ai.Ingredient{
				{Name: "Onion", Quantity: "1"},
				{Name: "Garlic", Quantity: ""},
				{Name: "onion", Quantity: "2"},
				{Name: "garlic", Quantity: "3 cloves"},
				{Name: "Basil", Quantity: " "},
				{Name: "  ", Quantity: "1"},
			},
			want: []*ai.Ingredient{
				{Name: "Onion", Quantity: "1, 2"},
				{Name: "Garlic", Quantity: "3 cloves"},
				{Name: "Basil", Quantity: ""},
			},
		},
	}

	input := []ai.InputIngredient{
		{
			Description: "Onion",
			AisleNumber: "1",
		},
		{
			Description: "Garlic",
			AisleNumber: "2",
		},
		{
			Description: "Basil",
			AisleNumber: "3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shoppingListForDisplay(tc.ingredients, input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestShoppingListForDisplay_SortsByAisleWithMissingAtBottom(t *testing.T) {
	ingredients := []ai.Ingredient{
		{Name: "Pantry Salt", Quantity: "1 tsp"},
		{Name: "Aisle Ten Rice", Quantity: "1 cup"},
		{Name: "Aisle Two Beans", Quantity: "1 can"},
		{Name: "Basil", Quantity: "1 bunch"},
		{Name: "Butter", Quantity: "2 tbsp"},
	}
	inputs := []ai.InputIngredient{
		{Description: "aisle ten rice", AisleNumber: "10"},
		{Description: "Aisle Two Beans", AisleNumber: "2"},
		{Description: "Basil", AisleNumber: "fresh-herbs"},
		{Description: "Butter", AisleNumber: "dairy-eggs"},
	}

	got := shoppingListForDisplay(ingredients, inputs)
	assert.Equal(t, []*ai.Ingredient{
		{Name: "Aisle Two Beans", Quantity: "1 can"},
		{Name: "Aisle Ten Rice", Quantity: "1 cup"},
		{Name: "Butter", Quantity: "2 tbsp"},
		{Name: "Basil", Quantity: "1 bunch"},
		{Name: "Pantry Salt", Quantity: "1 tsp"},
	}, got)
}

func TestShoppingListForDisplay_PreservesFirstSeenOrderWithinSameAisleState(t *testing.T) {
	ingredients := []ai.Ingredient{
		{Name: "Salt", Quantity: "1 tsp"},
		{Name: "Pepper", Quantity: "1 tsp"},
		{Name: "salt", Quantity: "1 pinch"},
		{Name: "Oil", Quantity: "2 tbsp"},
	}

	got := shoppingListForDisplay(ingredients, nil)

	assert.Equal(t, []*ai.Ingredient{
		{Name: "Salt", Quantity: "1 tsp, 1 pinch"},
		{Name: "Pepper", Quantity: "1 tsp"},
		{Name: "Oil", Quantity: "2 tbsp"},
	}, got)
}
