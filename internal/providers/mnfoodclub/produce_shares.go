package mnfoodclub

import (
	"strings"

	"careme/internal/ai"
)

// produceShareIngredients is the static inventory transcribed from the supplied
// November 2–7 share list (page 1 of the weekly feature PDF; later pages include
// older weeks). Share membership is part of each description and product ID.
func produceShareIngredients() []ai.InputIngredient {
	shares := []struct {
		name          string
		items         []string
		substitutions []string
	}{
		{
			name: "Seasonal",
			items: []string{
				"Acorn Squash", "Red Bell Peppers", "Green Napa Cabbage", "Brussels Sprouts",
				"Fennel", "Purple Turnips", "Yellow Candy Onions",
			},
		},
		{
			name: "Small Seasonal",
			items: []string{
				"Acorn Squash", "Green Bell Peppers", "Green Napa Cabbage", "Shallots",
				"Purple Turnips", "Yellow Candy Onions",
			},
		},
		{
			name: "Low Carb",
			items: []string{
				"Roma Tomatoes", "Yellow Keeper Onions", "Poblano Peppers", "Garlic",
				"Red Greentop Radish", "Cilantro", "Eggplant", "Collard Greens",
			},
		},
		{
			name: "Staple",
			items: []string{
				"Parsnips", "Leeks", "Carrots", "Celery", "Chiogga Beets",
			},
		},
		{
			name: "Fruit",
			items: []string{
				"Navel Oranges", "Green Grapes", "Grapefruit", "Avocado", "Key Limes", "Kiwi",
			},
		},
		{
			name: "Small Fruit",
			items: []string{
				"Navel Oranges", "Green Grapes", "Grapefruit", "Avocado",
			},
		},
	}
	var ingredients []ai.InputIngredient
	for _, share := range shares {
		for _, name := range append(share.items, share.substitutions...) {
			description := "Produce share " + strings.ToLower(share.name) + " " + name
			ingredient := ai.InputIngredient{
				ProductID:   "mnfoodclub" + strings.ToLower(strings.ReplaceAll(description, " ", "-")),
				Description: description,
				Brand:       "mnfood.club",
				Categories:  []string{"Produce", share.name},
				Grade:       &ai.IngredientGrade{Score: 10, Reason: "Fresh produce from the MNFoodClub produce share list."},
			}
			for _, substitution := range share.substitutions {
				if name == substitution {
					ingredient.Description += " (substitution)"
					ingredient.Categories = append(ingredient.Categories, "Substitution")
				}
			}
			ingredients = append(ingredients, ingredient)
		}
	}
	return ingredients
}
