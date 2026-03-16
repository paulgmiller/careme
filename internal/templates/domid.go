package templates

import "strings"

// SafeDOMIDComponent normalizes dynamic values so they can be embedded in HTML ids
// and reused inside selector-based HTMX attributes.
func SafeDOMIDComponent(value string) string {
	trimmed := strings.TrimSpace(strings.TrimRight(value, "="))
	if trimmed == "" {
		return "item"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, trimmed)
}
