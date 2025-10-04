package recipes

import (
	"careme/internal/config"
	"careme/internal/html"
	"careme/internal/locations"
	"careme/internal/users"
	"embed"
	"html/template"
	"io"
)

//go:embed templates/*.html
var templatesFS embed.FS

var templates *template.Template

func init() {
	templates = template.Must(template.New("").Funcs(template.FuncMap{
		"add": func(a, b int) int { return a + b },
	}).ParseFS(templatesFS, "templates/*.html"))
	templates = html.MustLoadSharedTemplates(templates)
}

// FormatChatHTML renders the raw AI chat (JSON or free-form text) for a location.
func FormatChatHTML(cfg *config.Config, p *generatorParams, chat []byte, writer io.Writer, user *users.User) error {
	data := struct {
		Location      locations.Location
		Date          string
		Chat          template.HTML
		ClarityScript template.HTML
		Instructions  string
		User          *users.User
	}{
		Location:      *p.Location,
		Date:          p.Date.Format("2006-01-02"),
		Chat:          template.HTML(chat),
		ClarityScript: html.ClarityScript(cfg),
		Instructions:  p.Instructions,
		User:          user,
	}
	return templates.ExecuteTemplate(writer, "chat.html", data)
}
