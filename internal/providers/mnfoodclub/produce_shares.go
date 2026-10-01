package mnfoodclub

import (
	"strings"

	"careme/internal/ai"
)

// produceShareIngredients is the static inventory transcribed from the supplied
// share list. Share membership is part of each description and product ID.
func produceShareIngredients() []ai.InputIngredient {
	shares := []struct {
		name          string
		items         []string
		substitutions []string
	}{
		{
			name: "Seasonal",
			items: []string{
				"Garlic", "Carrots", "Yellow Candy Onions", "Anaheim Peppers",
				"French Breakfast Radish", "Arugula", "Red Cabbage",
				"Yukon Gold Potatoes", "Cremini Mushrooms",
			},
		},
		{
			name: "Small Seasonal",
			items: []string{
				"Garlic", "Carrots", "Yellow Candy Onions", "Anaheim Peppers",
				"Koginut Squash", "Arugula", "French Breakfast Radish",
			},
			substitutions: []string{"Collard Greens"},
		},
		{
			name: "Low Carb",
			items: []string{
				"Celeriac", "Mini Cucumbers", "Heirloom Tomatoes", "Leeks", "Spaghetti Squash",
			},
		},
		{
			name: "Staple",
			items: []string{
				"Dino Kale", "Gold Beets", "Watermelon Radishes", "Parsnips", "Red Cipollini Onions",
			},
		},
		{
			name: "Fruit",
			items: []string{
				"Table Apples (1sts)", "Bartlett Pears", "Pomegranate", "Meyer Lemons", "Grapefruit",
			},
		},
		{
			name: "Small Fruit",
			items: []string{
				"Table Apples (1sts)", "Bartlett Pears", "Pomegranate", "Grapefruit",
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
