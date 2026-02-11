package recipes

import (
	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	"html/template"
	"io"
	"net/http"
	"slices"
	"strings"
)

// FormatShoppingListHTML renders the multi-recipe shopping list view.
func FormatShoppingListHTML(p *generatorParams, l ai.ShoppingList, signedIn bool, writer http.ResponseWriter) {
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
		ServerSignedIn bool
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
		ServerSignedIn: signedIn,
	}

	if err := templates.ShoppingList.Execute(writer, data); err != nil {
		http.Error(writer, "shopping list template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// FormatRecipeHTML renders a single recipe view.
func FormatRecipeHTML(p *generatorParams, recipe ai.Recipe, signedIn bool, thread []RecipeThreadEntry, writer http.ResponseWriter) {
	data := struct {
		Location       locations.Location
		Date           string
		ClarityScript  template.HTML
		Recipe         ai.Recipe
		OriginHash     string
		ConversationID string
		Thread         []RecipeThreadEntry
		RecipeHash     string
		Style          seasons.Style
		ServerSignedIn bool
	}{
		Location:       *p.Location,
		Date:           p.Date.Format("2006-01-02"),
		ClarityScript:  templates.ClarityScript(),
		Recipe:         recipe,
		OriginHash:     recipe.OriginHash,
		ConversationID: p.ConversationID,
		Thread:         thread,
		RecipeHash:     recipe.ComputeHash(),
		Style:          seasons.GetCurrentStyle(),
		ServerSignedIn: signedIn,
	}

	if err := templates.Recipe.Execute(writer, data); err != nil {
		http.Error(writer, "recipe template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// FormatRecipeThreadHTML renders the question thread fragment for HTMX swaps.
func FormatRecipeThreadHTML(thread []RecipeThreadEntry, signedIn bool, conversationID string, writer http.ResponseWriter) {
	//memory waste because we alwways resort?
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	data := struct {
		ConversationID string
		Thread         []RecipeThreadEntry
		ServerSignedIn bool
	}{
		ConversationID: conversationID,
		Thread:         thread,
		ServerSignedIn: signedIn,
	}

	if err := templates.Recipe.ExecuteTemplate(writer, "recipe_thread", data); err != nil {
		http.Error(writer, "recipe thread template error: "+err.Error(), http.StatusInternalServerError)
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
