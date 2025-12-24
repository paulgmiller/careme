package recipes

import (
	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	"context"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"net/http"
)

const recipeCachePrefix = "recipe/"

type getcache interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

type HtmlFromCache struct {
	Cache getcache
}

func (h HtmlFromCache) SingleFromCache(ctx context.Context, hash string) (*ai.Recipe, error) {
	recipe, err := h.Cache.Get(ctx, recipeCachePrefix+hash)
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

func (h HtmlFromCache) FromCache(ctx context.Context, hash string) (*ai.ShoppingList, error) {
	shoppinglist, err := h.Cache.Get(ctx, hash)
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
		http.Error(writer, "recipe not found or expired", http.StatusNotFound)
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
