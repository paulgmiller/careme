package recipes

import (
	"bytes"
	"careme/internal/config"
	"careme/internal/html"
	"careme/internal/locations"
	"careme/internal/users"
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templatesFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"add": func(a, b int) int { return a + b },
}).ParseFS(templatesFS, "templates/*.html"))

// FormatChatHTML renders the raw AI chat (JSON or free-form text) for a location.
func FormatChatHTML(cfg *config.Config, p *generatorParams, chat string, user *users.User) string {
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
	var buf bytes.Buffer
	_ = templates.ExecuteTemplate(&buf, "chat.html", data)
	return buf.String()
}
