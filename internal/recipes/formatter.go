package recipes

import (
	"fmt"
	"strings"

	"careme/internal/history"
)

type Formatter struct{}

func NewFormatter() *Formatter {
	return &Formatter{}
}

func (f *Formatter) FormatRecipes(recipes []history.Recipe) string {
	var output strings.Builder

	output.WriteString("🍽️  CAREME WEEKLY RECIPES\n")
	output.WriteString(strings.Repeat("=", 50) + "\n\n")

	for i, recipe := range recipes {
		output.WriteString(fmt.Sprintf("📋 RECIPE %d: %s\n", i+1, strings.ToUpper(recipe.Name)))
		output.WriteString(strings.Repeat("-", 30) + "\n")

		if recipe.Description != "" {
			output.WriteString(fmt.Sprintf("Description: %s\n\n", recipe.Description))
		}

		output.WriteString("🛒 INGREDIENTS:\n")
		for _, ingredient := range recipe.Ingredients {
			output.WriteString(fmt.Sprintf("  • %s\n", ingredient))
		}
		output.WriteString("\n")

		output.WriteString("👩‍🍳 INSTRUCTIONS:\n")
		for j, instruction := range recipe.Instructions {
			output.WriteString(fmt.Sprintf("  %d. %s\n", j+1, instruction))
		}

		if i < len(recipes)-1 {
			output.WriteString("\n" + strings.Repeat("=", 50) + "\n\n")
		}
	}

	output.WriteString("\n" + strings.Repeat("=", 50) + "\n")
	output.WriteString("🎯 Generated with fresh, seasonal ingredients!\n")
	output.WriteString("📍 Sourced from your local QFC/Fred Meyer\n")

	return output.String()
}

func (f *Formatter) FormatRecipeList(recipes []history.Recipe) string {
	var output strings.Builder

	output.WriteString("📋 This Week's Recipes:\n")
	for i, recipe := range recipes {
		output.WriteString(fmt.Sprintf("  %d. %s\n", i+1, recipe.Name))
	}

	return output.String()
}
