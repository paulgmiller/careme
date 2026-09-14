package recipes

import (
	"io"
	"strings"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
)

type mailRecipeView struct {
	ai.Recipe
	Hash            string
	PropertyDisplay recipePropertyDisplay
}

type mailView struct {
	Location       locations.Location
	Date           string
	Hash           string
	Recipes        []mailRecipeView
	Domain         string
	UnsubscribeURL string
	Style          seasons.Style
}

// FormatMail renders the recipe email using the configured public origin.
// TODO move this over to internal/mail once recipePropertyDisplay is in shared helper?
func FormatMail(p *generatorParams, l ai.ShoppingList, publicOrigin string, unsubscribeURL string, writer io.Writer) error {
	view := newMailView(p, l, publicOrigin, unsubscribeURL)
	return templates.Mail.Execute(writer, view)
}

func newMailView(p *generatorParams, list ai.ShoppingList, publicOrigin string, unsubscribeURL string) mailView {
	recipeViews := make([]mailRecipeView, 0, len(list.Recipes))
	for _, recipe := range list.Recipes {
		hash := recipe.ComputeHash()
		propertyDisplay := newRecipePropertyDisplay(recipe)
		propertyDisplay.Servings = strings.TrimSuffix(strings.TrimSuffix(propertyDisplay.Servings, " servings"), " serving")
		recipeViews = append(recipeViews, mailRecipeView{
			Recipe:          recipe,
			Hash:            hash,
			PropertyDisplay: propertyDisplay,
		})
	}

	return mailView{
		Location:       *p.Location,
		Date:           p.Date.Format("2006-01-02"),
		Hash:           p.Hash(),
		Recipes:        recipeViews,
		Domain:         publicOrigin,
		UnsubscribeURL: unsubscribeURL,
		Style:          seasons.GetCurrentStyle(),
	}
}
