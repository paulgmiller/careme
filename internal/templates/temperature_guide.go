package templates

import "careme/internal/seasons"

type TemperatureGuidePageData struct {
	Style seasons.Style
}

func NewTemperatureGuidePageData(style seasons.Style) TemperatureGuidePageData {
	return TemperatureGuidePageData{Style: style}
}
