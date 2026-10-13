package ai

import (
	"regexp"
	"strings"
)

var (
	catalogSizeSuffix = regexp.MustCompile(`(?i)(?:^|,| - )\s*((?:avg\.?\s+)?\d+(?:\.\d+)?\s*(?:fl\.?\s*oz|fluid ounces?|ounces?|oz|pounds?|lbs?|kilograms?|kg|grams?|g|milliliters?|ml|liters?|l|ct|count)|qty\s+\d+)\s*$`)
	catalogSizeRange  = regexp.MustCompile(`(?i)\d\s*(?:[a-z]+\s*)?[-–]\s*\d`)
)

// CatalogSizeFromDescription returns an explicit trailing catalog size, or an
// empty string when the description does not identify one. Average weights stay
// marked as averages; bare counts and product numbers are not inferred as sizes.
func CatalogSizeFromDescription(description string) string {
	if catalogSizeRange.MatchString(description) {
		return ""
	}
	match := catalogSizeSuffix.FindStringSubmatch(strings.TrimSpace(description))
	if match == nil {
		return ""
	}
	size := strings.TrimSpace(match[1])
	if strings.HasPrefix(strings.ToLower(size), "qty") {
		return strings.TrimSpace(size[3:]) + " count"
	}
	return size
}
