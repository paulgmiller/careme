package main

import (
	"embed"
	"html/template"
	"log"
)

//go:embed html/*.html
var htmlFiles embed.FS

// loadTemplates parses embedded templates at server startup instead of using init.
func loadTemplates() (home, spin *template.Template) {
	// Load the login widget template first
	widgetBytes, err := htmlFiles.ReadFile("html/login-widget.html")
	if err != nil {
		log.Fatalf("failed to read embedded login-widget.html: %v", err)
	}

	homeBytes, err := htmlFiles.ReadFile("html/home.html")
	if err != nil {
		log.Fatalf("failed to read embedded home.html: %v", err)
	}
	homeTmpl := template.Must(template.New("home").Parse(string(widgetBytes)))
	homeTmpl = template.Must(homeTmpl.Parse(string(homeBytes)))

	spinBytes, err := htmlFiles.ReadFile("html/spinner.html")
	if err != nil {
		log.Fatalf("failed to read embedded spinner.html: %v", err)
	}
	spinTmpl := template.Must(template.New("spinner").Parse(string(widgetBytes)))
	spinTmpl = template.Must(spinTmpl.Parse(string(spinBytes)))

	return homeTmpl, spinTmpl
}
