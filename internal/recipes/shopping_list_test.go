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
		want        []shoppingListGroup
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
			want: []shoppingListGroup{
				{
					Aisle: "Other items",
					Items: []*ai.Ingredient{
						{Name: "Onion", Quantity: "1, 2"},
						{Name: "Garlic", Quantity: "3 cloves"},
						{Name: "Basil", Quantity: ""},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shoppingListForDisplay(tc.ingredients)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestShoppingListForDisplay_SortsByAisleWithMissingAtBottom(t *testing.T) {
	ingredients := []ai.Ingredient{
		{Name: "Pantry Salt", Quantity: "1 tsp"},
		{Name: "Aisle Ten Rice", Quantity: "1 cup", AisleNumber: "10"},
		{Name: "Aisle Two Beans", Quantity: "1 can", AisleNumber: "2"},
		{Name: "Basil", Quantity: "1 bunch", AisleNumber: "fresh-herbs"},
		{Name: "Butter", Quantity: "2 tbsp", AisleNumber: "dairy-eggs"},
	}

	got := shoppingListForDisplay(ingredients)
	assert.Equal(t, []shoppingListGroup{
		{
			Aisle: "Aisle 2",
			Items: []*ai.Ingredient{
				{Name: "Aisle Two Beans", Quantity: "1 can", AisleNumber: "2"},
			},
		},
		{
			Aisle: "Aisle 10",
			Items: []*ai.Ingredient{
				{Name: "Aisle Ten Rice", Quantity: "1 cup", AisleNumber: "10"},
			},
		},
		{
			Aisle: "Dairy & eggs",
			Items: []*ai.Ingredient{
				{Name: "Butter", Quantity: "2 tbsp", AisleNumber: "dairy-eggs"},
			},
		},
		{
			Aisle: "Fresh herbs",
			Items: []*ai.Ingredient{
				{Name: "Basil", Quantity: "1 bunch", AisleNumber: "fresh-herbs"},
			},
		},
		{
			Aisle: "Other items",
			Items: []*ai.Ingredient{
				{Name: "Pantry Salt", Quantity: "1 tsp"},
			},
		},
	}, got)
}

func TestShoppingListForDisplay_PreservesFirstSeenOrderWithinSameAisleState(t *testing.T) {
	ingredients := []ai.Ingredient{
		{Name: "Salt", Quantity: "1 tsp"},
		{Name: "Pepper", Quantity: "1 tsp"},
		{Name: "salt", Quantity: "1 pinch"},
		{Name: "Oil", Quantity: "2 tbsp"},
	}

	got := shoppingListForDisplay(ingredients)

	assert.Equal(t, []shoppingListGroup{
		{
			Aisle: "Other items",
			Items: []*ai.Ingredient{
				{Name: "Salt", Quantity: "1 tsp, 1 pinch"},
				{Name: "Pepper", Quantity: "1 tsp"},
				{Name: "Oil", Quantity: "2 tbsp"},
			},
		},
	}, got)
}

func TestShoppingListForDisplay_KeepsFirstIngredientMetadataWhenCombining(t *testing.T) {
	ingredients := []ai.Ingredient{
		{ProductID: "lemon-1", Name: "Lemon", Quantity: "1", AisleNumber: "Produce", Price: "$2.00"},
		{ProductID: "lemon-1", Name: "lemon", Quantity: "1 tbsp juice", AisleNumber: "Produce", Price: "$2.00"},
	}

	got := shoppingListForDisplay(ingredients)

	assert.Equal(t, []shoppingListGroup{
		{
			Aisle: "Produce",
			Items: []*ai.Ingredient{
				{ProductID: "lemon-1", Name: "Lemon", Quantity: "1, 1 tbsp juice", AisleNumber: "Produce", Price: "$2.00"},
			},
		},
	}, got)
}

func TestShoppingListForDisplay_GroupsSortedItemsByAisle(t *testing.T) {
	got := shoppingListForDisplay([]ai.Ingredient{
		{Name: "Salt", Quantity: "1 tsp"},
		{Name: "Rice", Quantity: "1 cup", AisleNumber: "10"},
		{Name: "Beans", Quantity: "1 can", AisleNumber: "2"},
		{Name: "Butter", Quantity: "2 tbsp", AisleNumber: "dairy-eggs"},
		{Name: "Milk", Quantity: "1 cup", AisleNumber: "dairy-eggs"},
		{Name: "Basil", Quantity: "1 bunch", AisleNumber: "fresh-herbs"},
	})

	assert.Equal(t, []shoppingListGroup{
		{
			Aisle: "Aisle 2",
			Items: []*ai.Ingredient{
				{Name: "Beans", Quantity: "1 can", AisleNumber: "2"},
			},
		},
		{
			Aisle: "Aisle 10",
			Items: []*ai.Ingredient{
				{Name: "Rice", Quantity: "1 cup", AisleNumber: "10"},
			},
		},
		{
			Aisle: "Dairy & eggs",
			Items: []*ai.Ingredient{
				{Name: "Butter", Quantity: "2 tbsp", AisleNumber: "dairy-eggs"},
				{Name: "Milk", Quantity: "1 cup", AisleNumber: "dairy-eggs"},
			},
		},
		{
			Aisle: "Fresh herbs",
			Items: []*ai.Ingredient{
				{Name: "Basil", Quantity: "1 bunch", AisleNumber: "fresh-herbs"},
			},
		},
		{
			Aisle: "Other items",
			Items: []*ai.Ingredient{
				{Name: "Salt", Quantity: "1 tsp"},
			},
		},
	}, got)
}
