package recipes

import (
	"careme/internal/ai"
	"careme/internal/html"
	"careme/internal/locations"
	"careme/internal/templates"
	"context"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"time"
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

func (g *Generator) FromCache(ctx context.Context, hash string, p *generatorParams, w io.Writer) error {
	shoppinglist, err := g.cache.Get(hash) //this hash prefix is dumb now.
	if err != nil {
		return err
	}
	defer shoppinglist.Close()

	var list ai.ShoppingList
	err = json.NewDecoder(shoppinglist).Decode(&list)
	if err != nil {
		slog.ErrorContext(ctx, "failed to read cached recipe for hash", "hash", hash, "error", err)
		return err
	}

	// Load the params to properly format the HTML
	if p == nil {
		var err error
		p, err = g.LoadParamsFromHash(hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load params for hash", "hash", hash, "error", err)
			p = DefaultParams(&locations.Location{
				ID:   "",
				Name: "Unknown Location",
			}, time.Now())
		}
	}

	slog.InfoContext(ctx, "serving shared recipe by hash", "hash", hash)
	if err := g.FormatChatHTML(p, list, w); err != nil {
		slog.ErrorContext(ctx, "failed to format shared recipe for hash", "hash", hash, "error", err)
		return err
	}
	return nil
}

// FormatChatHTML renders the raw AI chat (JSON or free-form text) for a location.
func (g *Generator) FormatChatHTML(p *generatorParams, l ai.ShoppingList, writer io.Writer) error {
	//TODO just put prams into shopping list and pass that up?
	data := struct {
		Location      locations.Location
		Date          string
		ClarityScript template.HTML
		Instructions  string
		ResponseID    string
		Hash          string
		Recipes       []ai.Recipe
	}{
		Location:      *p.Location,
		Date:          p.Date.Format("2006-01-02"),
		ClarityScript: html.ClarityScript(g.config),
		Instructions:  p.Instructions,
		ResponseID:    l.ResponseID,
		Hash:          p.Hash(),
		Recipes:       l.Recipes,
	}

	return templates.Recipe.Execute(writer, data)
}
