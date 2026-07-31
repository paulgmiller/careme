package geo

import (
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
