package recipes

import (
	"fmt"
	"strings"

	"careme/internal/ai"
)

type cookingMethodDisplay struct {
	Label string
	Emoji string
}

type recipePropertyDisplay struct {
	Time           string
	Servings       string
	Cost           string
	Calories       string
	CookingMethods []cookingMethodDisplay
}

// newRecipePropertyDisplay also supports recipes cached before structured properties
// were introduced. A missing properties object decodes to RecipeProperties' zero value,
// and ranging over its nil CookingMethods slice is safe; unavailable values render as a
// dash while legacy time and cost strings remain available as fallbacks.
func newRecipePropertyDisplay(recipe ai.Recipe) recipePropertyDisplay {
	timeDisplay := strings.TrimSpace(recipe.CookTime)
	if recipe.Properties.TotalMinutes > 0 {
		timeDisplay = fmt.Sprintf("%d min", recipe.Properties.TotalMinutes)
	}
	if timeDisplay == "" {
		timeDisplay = "—"
	}

	servingsDisplay := "—"
	if recipe.Properties.Servings == 1 {
		servingsDisplay = "1 serving"
	} else if recipe.Properties.Servings > 1 {
		servingsDisplay = fmt.Sprintf("%d servings", recipe.Properties.Servings)
	}

	costDisplay := strings.TrimSpace(recipe.CostEstimate)
	if recipe.Properties.EstimatedCostDollars > 0 {
		costDisplay = fmt.Sprintf("$%d", recipe.Properties.EstimatedCostDollars)
	}
	if costDisplay == "" {
		costDisplay = "—"
	}

	caloriesDisplay := "—"
	if recipe.Properties.CaloriesPerServing > 0 {
		caloriesDisplay = fmt.Sprintf("%d cal", recipe.Properties.CaloriesPerServing)
	}

	methods := make([]cookingMethodDisplay, 0, len(recipe.Properties.CookingMethods))
	for _, method := range recipe.Properties.CookingMethods {
		display := newCookingMethodDisplay(method)
		if display.Label == "" {
			continue
		}
		methods = append(methods, display)
	}

	return recipePropertyDisplay{
		Time:           timeDisplay,
		Servings:       servingsDisplay,
		Cost:           costDisplay,
		Calories:       caloriesDisplay,
		CookingMethods: methods,
	}
}

func newCookingMethodDisplay(method ai.CookingMethod) cookingMethodDisplay {
	switch method {
	case ai.CookingMethodStovetop:
		return cookingMethodDisplay{Label: "Stovetop", Emoji: "🍳"}
	case ai.CookingMethodOven:
		return cookingMethodDisplay{Label: "Oven", Emoji: "♨️"}
	case ai.CookingMethodGrill:
		return cookingMethodDisplay{Label: "Grill", Emoji: "🔥"}
	case ai.CookingMethodSlowCooker:
		return cookingMethodDisplay{Label: "Slow cooker", Emoji: "🍲"}
	case ai.CookingMethodAirFryer:
		return cookingMethodDisplay{Label: "Air fryer", Emoji: "🌀"}
	case ai.CookingMethodNoCook:
		return cookingMethodDisplay{Label: "No-cook", Emoji: "🥗"}
	case ai.CookingMethodOther:
		return cookingMethodDisplay{Label: "Other", Emoji: "❓"}
	default:
		return cookingMethodDisplay{}
	}
}

func ingredientsForDisplay(base []ai.Ingredient, wineRecommendation *ai.WineSelection) []ai.Ingredient {
	display := make([]ai.Ingredient, 0, len(base))
	display = append(display, base...)
	if wineRecommendation == nil || len(wineRecommendation.Wines) == 0 {
		return display
	}
	display = append(display, wineRecommendation.Wines[0]) // Need a way to let the user pick among wines.
	return display
}
