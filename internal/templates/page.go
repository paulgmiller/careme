package templates

import (
	"context"
	"html/template"

	"careme/internal/seasons"
)

// Page holds shared presentation data for full HTML pages. Embed it in page
// views and populate it when constructing the page.
type Page struct {
	ClarityScript   template.HTML
	GoogleTagScript template.HTML
	Style           seasons.Style
}

func NewPage(ctx context.Context) Page {
	return Page{
		ClarityScript:   ClarityScript(ctx),
		GoogleTagScript: GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
	}
}
