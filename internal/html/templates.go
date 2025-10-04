package html

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var sharedTemplatesFS embed.FS

// LoadSharedTemplates loads shared HTML templates like login-widget
func LoadSharedTemplates(tmpl *template.Template) (*template.Template, error) {
	return tmpl.ParseFS(sharedTemplatesFS, "templates/*.html")
}

// MustLoadSharedTemplates loads shared templates and panics on error
func MustLoadSharedTemplates(tmpl *template.Template) *template.Template {
	return template.Must(LoadSharedTemplates(tmpl))
}
