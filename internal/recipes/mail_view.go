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
func FormatMail(p *generatorParams, l ai.ShoppingList, publicOrigin string, unsubscribeURL string, writer io.Writer) error {
	return renderMail(writer, newMailView(mailViewInput{params: p, list: l, publicOrigin: publicOrigin, unsubscribeURL: unsubscribeURL, style: seasons.GetCurrentStyle()}))
}

type mailViewInput struct {
	params         *generatorParams
	list           ai.ShoppingList
	publicOrigin   string
	unsubscribeURL string
	style          seasons.Style
}

func newMailView(input mailViewInput) mailView {
	recipeViews := make([]mailRecipeView, 0, len(input.list.Recipes))
	for _, recipe := range input.list.Recipes {
		hash := recipe.ComputeHash()
		propertyDisplay := newRecipePropertyDisplay(recipe)
		propertyDisplay.Servings = strings.TrimSuffix(strings.TrimSuffix(propertyDisplay.Servings, " servings"), " serving")
		recipeViews = append(recipeViews, mailRecipeView{
			Recipe:          recipe,
			Hash:            hash,
			PropertyDisplay: propertyDisplay,
		})
	}

	data := mailView{
		Location:       *input.params.Location,
		Date:           input.params.Date.Format("2006-01-02"),
		Hash:           input.params.Hash(),
		Recipes:        recipeViews,
		Domain:         input.publicOrigin,
		UnsubscribeURL: input.unsubscribeURL,
		Style:          input.style,
	}

	return data
}

func renderMail(writer io.Writer, data mailView) error {
	return templates.Mail.Execute(writer, data)
}
