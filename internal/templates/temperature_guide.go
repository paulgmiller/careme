package templates

import "careme/internal/seasons"

type temperatureGuidePageData struct {
	Style seasons.Style
}

func NewTemperatureGuidePageData(style seasons.Style) temperatureGuidePageData {
	return temperatureGuidePageData{Style: style}
}
