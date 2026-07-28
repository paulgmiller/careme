package templates

import "careme/internal/seasons"

type PrivacyPageData struct {
	Style seasons.Style
}

func NewPrivacyPageData(style seasons.Style) PrivacyPageData {
	return PrivacyPageData{Style: style}
}
