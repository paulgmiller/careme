package geo

import (
	"strings"
	_ "time/tzdata" // Embed IANA data for minimal containers without system zoneinfo.

	"github.com/ugjka/latlong"
)

// TimezoneNameForCoordinates uses latlong's embedded, intentionally
// low-resolution timezone map. It is fast and avoids a network lookup, but can
// be inaccurate very close to timezone borders.
func TimezoneNameForCoordinates(coordinates Coordinate) (string, bool) {
	if coordinates.Valid() != nil {
		return "", false
	}
	name := latlong.LookupZoneName(coordinates.Lat, coordinates.Lon)
	return name, name != ""
}

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
