package recipes

import (
	"careme/internal/ai"
	"reflect"
	"testing"
)

func TestShoppingListForDisplay(t *testing.T) {
	tests := []struct {
		name    string
		recipes []ai.Recipe
		want    []ai.Ingredient
	}{
		{
			name: "single recipe returns nil",
			recipes: []ai.Recipe{
				{Title: "Solo"},
			},
			want: nil,
		},
		{
			name: "combines quantities and preserves first-seen order",
			recipes: []ai.Recipe{
				{
					Title: "First",
					Ingredients: []ai.Ingredient{
						{Name: "Onion", Quantity: "1"},
						{Name: "Garlic", Quantity: ""},
					},
				},
				{
					Title: "Second",
					Ingredients: []ai.Ingredient{
						{Name: "onion", Quantity: "2"},
						{Name: "garlic", Quantity: "3 cloves"},
						{Name: "Basil", Quantity: " "},
						{Name: "  ", Quantity: "1"},
					},
				},
			},
			want: []ai.Ingredient{
				{Name: "Onion", Quantity: "1, 2"},
				{Name: "Garlic", Quantity: "3 cloves"},
				{Name: "Basil", Quantity: ""},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shoppingListForDisplay(tc.recipes)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("shoppingListForDisplay() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
