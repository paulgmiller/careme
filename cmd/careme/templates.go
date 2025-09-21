package main

import (
	"embed"
	"html/template"
	"log"
)

//go:embed html/*.html
var htmlFiles embed.FS

// loadTemplates parses embedded templates at server startup instead of using init.
func loadTemplates() (*template.Template, []byte) {
	homeBytes, err := htmlFiles.ReadFile("html/home.html")
	if err != nil {
		log.Fatalf("failed to read embedded home.html: %v", err)
	}
	homeTmpl := template.Must(template.New("home").Parse(string(homeBytes)))

	spinBytes, err := htmlFiles.ReadFile("html/spinner.html")
	if err != nil {
		log.Fatalf("failed to read embedded spinner.html: %v", err)
	}
	return homeTmpl, spinBytes
}
