package recipes

import (
	"careme/internal/ai"
	"careme/internal/html"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	"context"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
)

func (g *Generator) SingleFromCache(ctx context.Context, hash string) (*ai.Recipe, error) {
	recipe, err := g.cache.Get("recipe/" + hash)
	if err != nil {
		return nil, err
	}
	defer recipe.Close()

	var singleRecipe ai.Recipe
	err = json.NewDecoder(recipe).Decode(&singleRecipe)
	if err != nil {
		return nil, err
	}
	return &singleRecipe, nil
}

func (g *Generator) FromCache(ctx context.Context, hash string) (*ai.ShoppingList, error) {
	shoppinglist, err := g.cache.Get(hash) //this hash prefix is dumb now.
	if err != nil {
		return nil, err
	}
	defer shoppinglist.Close()

	var list ai.ShoppingList
	err = json.NewDecoder(shoppinglist).Decode(&list)
	if err != nil {
		slog.ErrorContext(ctx, "failed to read cached recipe for hash", "hash", hash, "error", err)
		return nil, err
	}

	slog.InfoContext(ctx, "serving shared recipe by hash", "hash", hash)
	return &list, nil
}

// FormatChatHTML renders the raw AI chat (JSON or free-form text) for a location.
func (g *Generator) FormatChatHTML(p *generatorParams, l ai.ShoppingList, writer io.Writer) error {
	//TODO just put params into shopping list and pass that up?
	data := struct {
		Location       locations.Location
		Date           string
		ClarityScript  template.HTML
		Instructions   string
		Hash           string
		Recipes        []ai.Recipe
		ConversationID string
		Colors         seasons.ColorScheme
	}{
		Location:       *p.Location,
		Date:           p.Date.Format("2006-01-02"),
		ClarityScript:  html.ClarityScript(g.config),
		Instructions:   p.Instructions,
		Hash:           p.Hash(),
		Recipes:        l.Recipes,
		ConversationID: l.ConversationID,
		Colors:         seasons.GetCurrentColorScheme(),
	}

	return templates.Recipe.Execute(writer, data)
}

// drops clarity, instructions and most of shoppinglist
func FormatMail(p *generatorParams, l ai.ShoppingList, writer io.Writer) error {
	//TODO just put params into shopping list and pass that up?

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
