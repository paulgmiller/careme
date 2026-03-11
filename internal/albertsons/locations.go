package albertsons

import (
	"careme/internal/cache"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/samber/lo"
)

const maxLocationDistanceMiles = 20.0

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type LocationBackend struct {
	zipLookup centroidByZip
	byID      map[string]locationtypes.Location
}

func NewLocationBackend(ctx context.Context, c cache.ListCache, zipLookup centroidByZip) (*LocationBackend, error) {
	if c == nil {
		return nil, fmt.Errorf("list cache is required")
	}
	if zipLookup == nil {
		return nil, fmt.Errorf("zip centroid lookup is required")
	}

	summaries, err := loadCachedStoreSummaries(ctx, c)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]locationtypes.Location, len(summaries))
	for _, summary := range summaries {
		loc := storeSummaryToLocation(*summary)
		byID[loc.ID] = loc
	}

	return &LocationBackend{
		zipLookup: zipLookup,
		byID:      byID,
	}, nil
}

func (b *LocationBackend) IsID(locationID string) bool {
	return IsID(locationID)
}

func (b *LocationBackend) GetLocationByID(_ context.Context, locationID string) (*locationtypes.Location, error) {
	locationID = strings.TrimSpace(locationID)
	if !IsID(locationID) {
		return nil, fmt.Errorf("albertsons location id %q is invalid", locationID)
	}

	loc, exists := b.byID[locationID]
	if !exists {
		return nil, fmt.Errorf("albertsons location %q not found", locationID)
	}

	copy := loc
	return &copy, nil
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	centroid, ok := b.zipLookup.ZipCentroidByZIP(strings.TrimSpace(zipcode))
	if !ok {
		slog.WarnContext(ctx, "requested zip has no centroid; returning unsorted locations without distance filter", "zip", zipcode)
		return nil, nil
	}

	type ranked struct {
		location locationtypes.Location
		distance float64
	}

	var rankedLocations []ranked
	for _, loc := range b.byID {
		if loc.Lat == nil || loc.Lon == nil {
			continue
		}

		distance := geo.HaversineMiles(centroid.Lat, centroid.Lon, *loc.Lat, *loc.Lon)
		if distance > maxLocationDistanceMiles {
			continue
		}
		rankedLocations = append(rankedLocations, ranked{location: loc, distance: distance})
	}

	sort.SliceStable(rankedLocations, func(i, j int) bool {
		return rankedLocations[i].distance < rankedLocations[j].distance
	})

	return lo.Map(rankedLocations, func(item ranked, _ int) locationtypes.Location {
		return item.location
	}), nil
}
