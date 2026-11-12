package recipes

import (
	"io"
	"strings"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
)

type mailRecipeView struct {
	ai.Recipe
	Hash            string
	PropertyDisplay recipePropertyDisplay
}

// MailOptions supplies recipient-specific links and saved recipes for an email.
type MailOptions struct {
	UnsubscribeURL string
	SavedRecipes   []utypes.Recipe
}

type mailView struct {
	RecentRecipes  []utypes.Recipe
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
func FormatMail(p *generatorParams, l ai.ShoppingList, publicOrigin string, options MailOptions, writer io.Writer) error {
	view := newMailView(p, l, publicOrigin, options)
	return templates.Mail.Execute(writer, view)
}

// FormatMailText renders the same meal plan and prompts without HTML.
func FormatMailText(p *generatorParams, l ai.ShoppingList, publicOrigin string, options MailOptions, writer io.Writer) error {
	return templates.MailText.Execute(writer, newMailView(p, l, publicOrigin, options))
}

func newMailView(p *generatorParams, list ai.ShoppingList, publicOrigin string, options MailOptions) mailView {
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

	recentRecipes := make([]utypes.Recipe, 0)
	seen := make(map[string]bool)
	for _, recipe := range options.SavedRecipes {
		if recipe.CreatedAt.Before(p.Date.AddDate(0, 0, -7)) || recipe.CreatedAt.After(p.Date) || seen[recipe.Hash] {
			continue
		}
		seen[recipe.Hash] = true
		recentRecipes = append(recentRecipes, recipe)
	}
	return mailView{
		RecentRecipes:  recentRecipes,
		Location:       *p.Location,
		Date:           p.Date.Format("January 2, 2006"),
		Hash:           p.Hash(),
		Recipes:        recipeViews,
		Domain:         publicOrigin,
		UnsubscribeURL: options.UnsubscribeURL,
		Style:          seasons.GetCurrentStyle(),
	}
}
