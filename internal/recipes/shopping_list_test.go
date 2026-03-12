package recipes

import (
	"careme/internal/ai"
	"reflect"
	"testing"
)

func TestShoppingListForDisplay(t *testing.T) {
	tests := []struct {
		name        string
		ingredients []ai.Ingredient
		want        []ai.Ingredient
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
			want: []ai.Ingredient{
				{Name: "Onion", Quantity: "1, 2"},
				{Name: "Garlic", Quantity: "3 cloves"},
				{Name: "Basil", Quantity: ""},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shoppingListForDisplay(tc.ingredients)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("shoppingListForDisplay() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
