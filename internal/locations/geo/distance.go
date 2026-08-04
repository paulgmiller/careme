package geo

import "math"

// HaversineMiles returns the great-circle distance between two coordinates in
// statute miles.
func HaversineMiles(from, to Coordinate) float64 {
	const earthRadiusMiles = 3958.7613
	toRadians := math.Pi / 180.0

	dLat := (to.Lat - from.Lat) * toRadians
	dLon := (to.Lon - from.Lon) * toRadians
	lat1Rad := from.Lat * toRadians
	lat2Rad := to.Lat * toRadians

	sinHalfDLat := math.Sin(dLat / 2.0)
	sinHalfDLon := math.Sin(dLon / 2.0)
	a := sinHalfDLat*sinHalfDLat + math.Cos(lat1Rad)*math.Cos(lat2Rad)*sinHalfDLon*sinHalfDLon
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return earthRadiusMiles * c
}
