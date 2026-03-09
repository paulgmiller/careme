package geo

import "math"

// HaversineMiles returns great-circle distance between two latitude/longitude
// points in statute miles. Inputs are decimal degrees.
func HaversineMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMiles = 3958.7613
	toRadians := math.Pi / 180.0

	dLat := (lat2 - lat1) * toRadians
	dLon := (lon2 - lon1) * toRadians
	lat1Rad := lat1 * toRadians
	lat2Rad := lat2 * toRadians

	sinHalfDLat := math.Sin(dLat / 2.0)
	sinHalfDLon := math.Sin(dLon / 2.0)
	a := sinHalfDLat*sinHalfDLat + math.Cos(lat1Rad)*math.Cos(lat2Rad)*sinHalfDLon*sinHalfDLon
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return earthRadiusMiles * c
}
