package mnfoodclub

import (
	"strings"

	"careme/internal/ai"
)

// produceShareIngredients is the static inventory transcribed from the supplied
// October 19–24 share list (page 1 of the weekly feature PDF; later pages include
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
				"Koginut Squash", "Broccolini", "Carrots", "Daikon Radish",
				"Kennebec Potatoes", "Red Keeper Onions", "Poblano Peppers",
			},
		},
		{
			name: "Small Seasonal",
			items: []string{
				"Stripetti or Spaghetti Squash", "Poblano Peppers", "Carrots", "Cilantro",
				"Red Keeper Onions", "Pie Apples (2nds)",
			},
		},
		{
			name: "Low Carb",
			items: []string{
				"Mini Peppers", "Broccolini", "Zucchini", "Kale", "Red Cabbage",
			},
		},
		{
			name: "Staple",
			items: []string{
				"Jalapeño Peppers", "Red Potatoes", "Kohlrabi", "Green Cabbage", "Yellow Onions", "Cucumbers",
			},
		},
		{
			name: "Fruit",
			items: []string{
				"Mandarins", "Asian Pear", "Mango", "Cranberries", "Cantaloupe",
			},
		},
		{
			name: "Small Fruit",
			items: []string{
				"Mandarins", "Asian Pear", "Mango", "Cantaloupe",
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
