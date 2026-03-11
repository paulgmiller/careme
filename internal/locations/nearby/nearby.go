package nearby

import (
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
	"context"
	"log/slog"
	"sort"
	"strings"
)

type CentroidLookup interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

func FilterAndSortByZip(ctx context.Context, zipLookup CentroidLookup, zipcode string, candidates []locationtypes.Location, maxDistanceMiles float64) []locationtypes.Location {
	centroid, ok := zipLookup.ZipCentroidByZIP(strings.TrimSpace(zipcode))
	if !ok {
		slog.WarnContext(ctx, "requested zip has no centroid; returning unsorted locations without distance filter", "zip", zipcode)
		return nil
	}

	type ranked struct {
		location locationtypes.Location
		distance float64
	}

	var rankedLocations []ranked
	for _, loc := range candidates {
		if loc.Lat == nil || loc.Lon == nil {
			continue
		}

		distance := geo.HaversineMiles(centroid.Lat, centroid.Lon, *loc.Lat, *loc.Lon)
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
