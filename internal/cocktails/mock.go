package cocktails

import (
	"context"

	"careme/internal/ai"
	"careme/internal/seasons"
)

// Mock keeps cocktail development independent of external credentials.
type Mock struct{}

func (Mock) FetchCocktailIngredients(context.Context, string, seasons.Season) ([]ai.InputIngredient, error) {
	return []ai.InputIngredient{{ProductID: "gin", Description: "Gin"}, {ProductID: "lemon", Description: "Lemon"}, {ProductID: "mint", Description: "Fresh mint"}}, nil
}

func (Mock) GenerateCocktails(context.Context, string, []ai.InputIngredient) (*ai.CocktailMenu, error) {
	menu := &ai.CocktailMenu{}
	for _, name := range []string{"Garden Gin Sour", "Mint Gin Smash", "Long Lemon Cooler"} {
		menu.Cocktails = append(menu.Cocktails, ai.Cocktail{Title: name, Description: "A bright gin cocktail with fresh lemon and mint.", SeasonalNote: "Fresh herbs bring a garden accent to this citrus drink. Demo menu.", Glass: "Rocks glass", Ingredients: []ai.Ingredient{{ProductID: "gin", Name: "Gin", Quantity: "1½ oz"}, {ProductID: "lemon", Name: "Lemon juice", Quantity: "¾ oz"}, {ProductID: "mint", Name: "Mint", Quantity: "4 leaves"}, {Name: "Sugar", Quantity: "2 teaspoons"}, {Name: "Water", Quantity: "2 teaspoons"}, {Name: "Ice", Quantity: "1 cup"}}, Instructions: []string{"Dissolve 2 teaspoons sugar in 2 teaspoons water. Gently muddle 4 mint leaves in the syrup.", "Add 1½ oz gin, ¾ oz lemon juice and 1 cup ice. Shake and strain into a rocks glass."}})
	}
	return menu, nil
}
