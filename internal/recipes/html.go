package recipes

import (
	"careme/internal/html"
	"careme/internal/locations"
	"embed"
	"html/template"
	"io"
)

//go:embed templates/*.html
var templatesFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"add": func(a, b int) int { return a + b },
}).ParseFS(templatesFS, "templates/*.html"))

//TODO have a from cache where generator does more of the work in web.go

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
