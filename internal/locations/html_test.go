package locations

import (
	"bytes"
	"testing"

	"golang.org/x/net/html"
)

func isValidHTML(t *testing.T, htmlStr string) {
	if htmlStr == "" {
		t.Fatal("rendered HTML is empty")
	}
	_, err := html.Parse(bytes.NewBufferString(htmlStr))
	if err != nil {
		t.Fatalf("rendered HTML is not valid: %v\nHTML:\n%s", err, htmlStr)
	}
}

func TestLocationsHtml_ValidHTML(t *testing.T) {
	locs := []Location{
		{ID: "L1", Name: "Store One", Address: "100 Main St"},
		{ID: "L2", Name: "Store Two", Address: "200 Oak Ave"},
	}
	html := Html(locs, "12345")
	isValidHTML(t, html)
}
