package recipes

import (
	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	"html/template"
	"io"
	"net/http"
)

// FormatChatHTML renders the raw AI chat (JSON or free-form text) for a location.
func FormatChatHTML(p *generatorParams, l ai.ShoppingList, writer http.ResponseWriter) {
	// TODO just put params into shopping list and pass that up?
	data := struct {
		Location       locations.Location
		Date           string
		ClarityScript  template.HTML
		Instructions   string
		Hash           string
		Recipes        []ai.Recipe
		ConversationID string
		Style          seasons.Style
	}{
		Location:       *p.Location,
		Date:           p.Date.Format("2006-01-02"),
		ClarityScript:  templates.ClarityScript(),
		Instructions:   p.Instructions,
		Hash:           p.Hash(),
		Recipes:        l.Recipes,
		ConversationID: l.ConversationID,
		Style:          seasons.GetCurrentStyle(),
	}

	if err := templates.Recipe.Execute(writer, data); err != nil {
		http.Error(writer, "recipe template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// drops clarity, instructions and most of shoppinglist
func FormatMail(p *generatorParams, l ai.ShoppingList, writer io.Writer) error {
	// TODO just put params into shopping list and pass that up?

	data := struct {
		Location locations.Location
		Date     string
		Hash     string
		Recipes  []ai.Recipe
		Domain   string
	}{
		Location: *p.Location,
		Date:     p.Date.Format("2006-01-02"),
		Hash:     p.Hash(),
		Recipes:  l.Recipes,
		Domain:   "https://careme.cooking",
	}

	return templates.Mail.Execute(writer, data)
}
