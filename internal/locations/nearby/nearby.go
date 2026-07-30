package nearby

import (
	"sort"

	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

// rural a problem?
const MaxLocationDistanceMiles = 20.0

var MaxLocationCount = 10

func FilterAndSortByCoordinates(origin geo.Coordinate, candidates []locationtypes.Location, maxDistanceMiles float64) []locationtypes.Location {
	type ranked struct {
		location locationtypes.Location
		distance float64
	}

	var rankedLocations []ranked
	for _, loc := range candidates {
		if loc.Lat == nil || loc.Lon == nil {
			continue
		}

		distance := geo.HaversineMiles(origin, geo.Coordinate{Lat: *loc.Lat, Lon: *loc.Lon})
		if distance > maxDistanceMiles {
			continue
		}
		rankedLocations = append(rankedLocations, ranked{location: loc, distance: distance})
	}

	sort.SliceStable(rankedLocations, func(i, j int) bool {
		return rankedLocations[i].distance < rankedLocations[j].distance
	})

	out := make([]locationtypes.Location, 0, len(rankedLocations))
	for _, item := range rankedLocations {
		out = append(out, item.location)
	}
	return out
}
