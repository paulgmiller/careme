package recipes

import (
	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	"html/template"
	"io"
	"net/http"
	"strings"
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
		ShoppingList   []ai.Ingredient
		ConversationID string
		Style          seasons.Style
	}{
		Location:       *p.Location,
		Date:           p.Date.Format("2006-01-02"),
		ClarityScript:  templates.ClarityScript(),
		Instructions:   p.Instructions,
		Hash:           p.Hash(),
		Recipes:        l.Recipes,
		ShoppingList:   shoppingListForDisplay(l.Recipes),
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

func shoppingListForDisplay(recipes []ai.Recipe) []ai.Ingredient {
	if len(recipes) <= 1 {
		return nil
	}
	items := make(map[string]*ai.Ingredient)
	order := make([]string, 0)

	for _, recipe := range recipes {
		for _, ingredient := range recipe.Ingredients {
			name := strings.ToLower(strings.TrimSpace(ingredient.Name))
			if name == "" {
				continue
			}
			existing, ok := items[name]
			if !ok {
				items[name] = &ai.Ingredient{
					Name:     ingredient.Name,
					Quantity: strings.TrimSpace(ingredient.Quantity),
				}
				order = append(order, name)
				continue
			}
			qty := strings.TrimSpace(ingredient.Quantity)
			if qty == "" {
				continue
			}
			if existing.Quantity == "" {
				existing.Quantity = qty
				continue
			}
			existing.Quantity = existing.Quantity + ", " + qty
		}
	}

	combined := make([]ai.Ingredient, 0, len(order))
	for _, name := range order {
		combined = append(combined, *items[name])
	}
	return combined
}
