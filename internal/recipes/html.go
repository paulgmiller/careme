package recipes

import (
	"careme/internal/ai"
	"careme/internal/html"
	"careme/internal/locations"
	"context"
	"embed"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"time"
)

//go:embed templates/*.html
var templatesFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"add": func(a, b int) int { return a + b },
}).ParseFS(templatesFS, "templates/*.html"))

func (g *Generator) FromCache(ctx context.Context, hash string, p *generatorParams, w io.Writer) error {
	recipe, err := g.cache.Get(hash)
	if err != nil {
		return err
	}
	defer recipe.Close()

	recipebytes, err := io.ReadAll(recipe)
	if err != nil {
		slog.ErrorContext(ctx, "failed to read cached recipe for hash", "hash", hash, "error", err)
		return err
	}
	var list ai.ShoppingList // ensure import
	if err = json.Unmarshal(recipebytes, &list); err != nil {
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

	data := struct {
		Location      locations.Location
		Date          string
		ClarityScript template.HTML
		Instructions  string
		Hash          string
		Recipes       []ai.Recipe
	}{
		Location:      *p.Location,
		Date:          p.Date.Format("2006-01-02"),
		ClarityScript: html.ClarityScript(g.config),
		Instructions:  p.Instructions,
		Hash:          p.Hash(),
		Recipes:       l.Recipes,
	}
	return templates.ExecuteTemplate(writer, "chat.html", data)
}
