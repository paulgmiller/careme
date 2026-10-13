package mnfoodclub

import (
	"strings"

	"careme/internal/ai"
)

// produceShareIngredients is the static inventory transcribed from the supplied
// October 12–17 share list. Share membership is part of each description and product ID.
func produceShareIngredients() []ai.InputIngredient {
	shares := []struct {
		name          string
		items         []string
		substitutions []string
	}{
		{
			name: "Seasonal",
			items: []string{
				"Rutabaga", "Leeks", "Flat Leaf Parsley", "Watermelon Radish",
				"Parsnips", "Pie Apples (2nds)",
			},
		},
		{
			name: "Small Seasonal",
			items: []string{
				"Butternut Squash", "Yellow Onions", "Flat Leaf Parsley", "Garlic Chives",
				"Parsnips", "Table Apples (1sts)",
			},
		},
		{
			name: "Low Carb",
			items: []string{
				"Kohlrabi", "Asparagus", "Yellow Candy Onions", "Avocado", "Carmen Peppers",
			},
			substitutions: []string{"Cauliflower"},
		},
		{
			name: "Staple",
			items: []string{
				"Broccoli", "Russet Potatoes", "Red Beets", "Red Onions", "Koginut Squash",
			},
		},
		{
			name: "Fruit",
			items: []string{
				"Table Apples (1sts)", "Kiwi", "Bosc Pears", "Pineapple", "Red Grapes",
			},
		},
		{
			name: "Small Fruit",
			items: []string{
				"Table Apples (1sts)", "Kiwi", "Bosc Pears", "Red Grapes",
			},
		},
	}
	var ingredients []ai.InputIngredient
	for _, share := range shares {
		for _, name := range append(share.items, share.substitutions...) {
			description := "Produce share " + strings.ToLower(share.name) + " " + name
			ingredient := ai.InputIngredient{
				ProductID:   LocationIDPrefix + strings.ToLower(strings.ReplaceAll(description, " ", "-")),
				Description: description,
				Brand:       "mnfood.club",
				AisleNumber: "Produce",
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
