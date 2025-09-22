package recipes

import (
	"bytes"
	"careme/internal/config"
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
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: "test123"},
	}
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	chat := "<pre>{\"message\": \"hi\"}</pre>"
	html := FormatChatHTML(cfg, loc, time.Now(), chat)
	isValidHTML(t, html)
}

func TestFormatChatHTML_IncludesClarityScript(t *testing.T) {
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: "test456"},
	}
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	chat := "<pre>{\"message\": \"hi\"}</pre>"
	html := FormatChatHTML(cfg, loc, time.Now(), chat)

	if !bytes.Contains([]byte(html), []byte("www.clarity.ms/tag/")) {
		t.Error("HTML should contain Clarity script URL")
	}

	if !bytes.Contains([]byte(html), []byte("test456")) {
		t.Error("HTML should contain project ID")
	}
}

func TestFormatChatHTML_NoClarityWhenEmpty(t *testing.T) {
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: ""},
	}
	loc := locations.Location{ID: "L1", Name: "Store", Address: "1 Main St"}
	chat := "<pre>{\"message\": \"hi\"}</pre>"
	html := FormatChatHTML(cfg, loc, time.Now(), chat)

	if bytes.Contains([]byte(html), []byte("clarity.ms")) {
		t.Error("HTML should not contain Clarity script when project ID is empty")
	}
}
