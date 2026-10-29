package templates

import "careme/internal/seasons"

type privacyPageData struct {
	Style seasons.Style
}

func NewPrivacyPageData(style seasons.Style) privacyPageData {
	return privacyPageData{Style: style}
}
