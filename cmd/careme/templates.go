package main

import (
	"careme/internal/html"
	"embed"
	"html/template"
	"log"
)

//go:embed html/*.html
var htmlFiles embed.FS

// loadTemplates parses embedded templates at server startup instead of using init.
func loadTemplates() (home, spin, user *template.Template) {
	homeBytes, err := htmlFiles.ReadFile("html/home.html")
	if err != nil {
		log.Fatalf("failed to read embedded home.html: %v", err)
	}
	homeTmpl := template.Must(template.New("home").Parse(string(homeBytes)))
	homeTmpl = html.MustLoadSharedTemplates(homeTmpl)

	spinBytes, err := htmlFiles.ReadFile("html/spinner.html")
	if err != nil {
		log.Fatalf("failed to read embedded spinner.html: %v", err)
	}
	spinTmpl := template.Must(template.New("spinner").Parse(string(spinBytes)))
	spinTmpl = html.MustLoadSharedTemplates(spinTmpl)

	userBytes, err := htmlFiles.ReadFile("html/user.html")
	if err != nil {
		log.Fatalf("failed to read embedded user.html: %v", err)
	}
	userTmpl := template.Must(template.New("user").Parse(string(userBytes)))

	return homeTmpl, spinTmpl, userTmpl
}
