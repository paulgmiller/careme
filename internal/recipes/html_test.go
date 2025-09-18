package recipes

import (
	"bytes"
	"careme/internal/locations"
	"testing"
	"time"

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

func TestFormatChatHTML_ValidHTML(t *testing.T) {
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	chat := "<pre>{\"message\": \"hi\"}</pre>"
	html := FormatChatHTML(loc, time.Now(), chat)
	isValidHTML(t, html)
}
