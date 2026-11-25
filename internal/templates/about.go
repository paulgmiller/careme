package templates

import (
	"context"
	"html/template"

	"careme/internal/seasons"
)

type aboutPageData struct {
	ClarityScript   template.HTML
	GoogleTagScript template.HTML
	Style           seasons.Style
}

func NewAboutPageData(ctx context.Context, style seasons.Style) aboutPageData {
	return aboutPageData{
		ClarityScript:   ClarityScript(ctx),
		GoogleTagScript: GoogleTagScript(),
		Style:           style,
	}
}
