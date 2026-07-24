package geo

import "strings"

func TimezoneNameForZip(zip string) (string, bool) {
	trimmed := strings.TrimSpace(zip)
	if trimmed == "" {
		return "", false
	}
	switch first := trimmed[0]; {
	case first >= '0' && first <= '3':
		return "America/New_York", true
	case first >= '4' && first <= '7':
		return "America/Chicago", true
	case first == '8':
		return "America/Denver", true
	case first == '9':
		return "America/Los_Angeles", true
	default:
		return "", false
	}
}
