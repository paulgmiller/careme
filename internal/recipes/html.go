package recipes

import (
	"careme/internal/html"
	"careme/internal/locations"
	"embed"
	"html/template"
	"io"
	"log"
	"time"
)

//go:embed templates/*.html
var templatesFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"add": func(a, b int) int { return a + b },
}).ParseFS(templatesFS, "templates/*.html"))

func (g *Generator) FromCache(hash string, p *generatorParams, w io.Writer) error {
	recipe, err := g.cache.Get(hash)
	if err != nil {
		return err
	}
	defer recipe.Close()

	recipebytes, err := io.ReadAll(recipe)
	if err != nil {
		log.Printf("failed to read cached recipe for hash %s: %v", hash, err)
		return err
	}

	// Load the params to properly format the HTML
	if p == nil {
		var err error
		p, err = g.LoadParamsFromHash(hash)
		if err != nil {
			log.Printf("failed to load params for hash %s: %v", hash, err)
			p = DefaultParams(&locations.Location{
				ID:   "",
				Name: "Unknown Location",
			}, time.Now())
		}
	}

	log.Printf("serving shared recipe by hash: %s", hash)
	if err := g.FormatChatHTML(p, recipebytes, w); err != nil {
		log.Printf("failed to format shared recipe for hash %s: %v", hash, err)
		return err
	}
	return nil
}

// FormatChatHTML renders the raw AI chat (JSON or free-form text) for a location.
func (g *Generator) FormatChatHTML(p *generatorParams, chat []byte, writer io.Writer) error {
	data := struct {
		Location      locations.Location
		Date          string
		Chat          template.HTML
		ClarityScript template.HTML
		Instructions  string
		Hash          string
	}{
		Location:      *p.Location,
		Date:          p.Date.Format("2006-01-02"),
		Chat:          template.HTML(chat),
		ClarityScript: html.ClarityScript(g.config),
		Instructions:  p.Instructions,
		Hash:          p.Hash(),
	}
	return templates.ExecuteTemplate(writer, "chat.html", data)
}
