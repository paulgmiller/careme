package recipes

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"careme/internal/locations"
)

func TestFormatMail_ValidHTML(t *testing.T) {
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	var w bytes.Buffer
	recipeHash := list.Recipes[0].ComputeHash()
	if err := FormatMail(p, list, "https://careme.cooking", "https://careme.cooking/unsubscribe", &w); err != nil {
		t.Fatalf("FormatMail() error = %v", err)
	}
	html := w.String()

	isValidHTML(t, html)
	for _, want := range []string{
		"Test Recipe", "https://careme.cooking/cdn-cgi/image/width=752,quality=75,format=jpeg,onerror=redirect/recipe/" + recipeHash + "/image", "35 min", "👥&nbsp;4</span>", "$21", "540 cal", "🍳", "Stovetop", "♨️", "Oven",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("mail HTML should contain %q", want)
		}
	}
}
