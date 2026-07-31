package geo

import (
	_ "time/tzdata" // Embed IANA data for minimal containers without system zoneinfo.

	tz "github.com/ugjka/go-tz/v2"
)

// TimezoneNameForCoordinates uses go-tz's embedded, simplified timezone
// polygons. It avoids a network lookup, but can be inaccurate very close to
// timezone borders.
func TimezoneNameForCoordinates(coordinates Coordinate) (string, bool) {
	if coordinates.Valid() != nil {
		return "", false
	}
	names, err := tz.GetZone(tz.Point{Lat: coordinates.Lat, Lon: coordinates.Lon})
	if err != nil || len(names) == 0 {
		return "", false
	}
	return names[0], true
}
