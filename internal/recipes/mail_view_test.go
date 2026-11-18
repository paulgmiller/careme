package recipes

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatMail_ValidHTML(t *testing.T) {
	t.Parallel()
	loc := locations.Location{ID: "70000001", Name: "Store", Address: "1 Main St"}
	p := DefaultParams(&loc, time.Now())
	var w bytes.Buffer
	recipeHash := list.Recipes[0].ComputeHash()
	if err := FormatMail(p, list, "https://careme.cooking", MailOptions{UnsubscribeURL: "https://careme.cooking/unsubscribe"}, &w); err != nil {
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

func TestMailPromptsForRecentlySavedRecipes(t *testing.T) {
	date := time.Date(2026, time.November, 12, 9, 0, 0, 0, time.UTC)
	p := DefaultParams(&locations.Location{ID: "70000001", Name: "My Store"}, date)
	saved := []utypes.Recipe{
		{Title: "Recent dinner", Hash: "recent", CreatedAt: date.Add(-time.Hour)},
		{Title: "Duplicate dinner", Hash: "recent", CreatedAt: date.Add(-time.Hour)},
		{Title: "Boundary dinner", Hash: "boundary", CreatedAt: date.AddDate(0, 0, -7)},
		{Title: "Old dinner", Hash: "old", CreatedAt: date.AddDate(0, 0, -7).Add(-time.Second)},
		{Title: "Future dinner", Hash: "future", CreatedAt: date.Add(time.Second)},
	}
	for _, format := range []struct {
		name   string
		render func(*generatorParams, ai.ShoppingList, string, MailOptions, io.Writer) error
	}{{"html", FormatMail}, {"text", FormatMailText}} {
		t.Run(format.name, func(t *testing.T) {
			for _, hasSaved := range []bool{false, true} {
				options := MailOptions{UnsubscribeURL: "https://careme.cooking/user/unsubscribe?token=test&user=chef"}
				if hasSaved {
					options.SavedRecipes = saved
				}
				var buf bytes.Buffer
				require.NoError(t, format.render(p, list, "https://careme.cooking", options, &buf))
				body := buf.String()
				for _, want := range []string{"Your Careme meal plan", "November 12, 2026", "My Store", "Test Recipe", "A simple quail recipe", "35 min", "$21", "540 cal", "https://careme.cooking/recipe/" + list.Recipes[0].ComputeHash(), "https://careme.cooking/recipes?h=" + p.Hash(), "What ingredient would you like to cook next week? Hit reply and tell me.", "Unsubscribe"} {
					assert.Contains(t, body, want)
				}
				if hasSaved {
					assert.Contains(t, body, "How did your saved recipes turn out?")
					assert.Contains(t, body, "Recent dinner")
					assert.Contains(t, body, "Boundary dinner")
					assert.Contains(t, body, "https://careme.cooking/recipe/recent")
				} else {
					assert.NotContains(t, body, "How did your saved recipes turn out?")
					assert.NotContains(t, body, "Recent dinner")
				}
				for _, absent := range []string{"Duplicate dinner", "Old dinner", "Future dinner"} {
					assert.NotContains(t, body, absent)
				}
			}
		})
	}
}

func TestMailEscapesHTMLButPreservesPlainText(t *testing.T) {
	p := DefaultParams(&locations.Location{ID: "70000001", Name: "A & B"}, time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC))
	list := ai.ShoppingList{Recipes: []ai.Recipe{{Title: "Pasta & peas", Description: "A <fresh> dinner"}}}
	var html, text bytes.Buffer
	require.NoError(t, FormatMail(p, list, "https://careme.cooking", MailOptions{}, &html))
	require.NoError(t, FormatMailText(p, list, "https://careme.cooking", MailOptions{}, &text))
	assert.Contains(t, html.String(), "A &lt;fresh&gt; dinner")
	assert.NotContains(t, html.String(), "A <fresh> dinner")
	assert.Contains(t, text.String(), "A <fresh> dinner")
	assert.Contains(t, text.String(), "Pasta & peas")
}

type failedMailWriter struct{}

func (failedMailWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestMailReturnsRenderErrors(t *testing.T) {
	p := DefaultParams(&locations.Location{ID: "70000001"}, time.Now())
	assert.ErrorIs(t, FormatMail(p, list, "https://careme.cooking", MailOptions{}, failedMailWriter{}), io.ErrClosedPipe)
	assert.ErrorIs(t, FormatMailText(p, list, "https://careme.cooking", MailOptions{}, failedMailWriter{}), io.ErrClosedPipe)
}
