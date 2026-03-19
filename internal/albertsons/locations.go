package albertsons

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations/geo"
	"careme/internal/locations/nearby"
	"careme/internal/locations/pointindex"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	locationtypes "careme/internal/locations/types"
)

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type LocationBackend struct {
	zipLookup  centroidByZip
	cache      cache.ListCache
	pointIndex map[string]pointindex.Point
}

func NewLocationBackendFromConfig(ctx context.Context, cfg *config.Config, zipLookup centroidByZip) (*LocationBackend, error) {
	if !cfg.Albertsons.IsEnabled() {
		return nil, locationtypes.DisabledBackendError("Albertsons")
	}

	if zipLookup == nil {
		return nil, fmt.Errorf("zip centroid lookup is required")
	}

	listCache, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create Albertsons list cache: %w", err)
	}

	return newLocationBackend(ctx, listCache, zipLookup)
}

func newLocationBackend(ctx context.Context, c cache.ListCache, zipLookup centroidByZip) (*LocationBackend, error) {
	pointIndex, err := pointindex.LoadOrBuild(ctx, c, LoadCachedStoreSummaries)
	if err != nil {
		return nil, err
	}

	keys, err := c.List(ctx, StoreCachePrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list cached store summaries: %w", err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("failed to load albertsons locations")
	}

	return &LocationBackend{
		zipLookup:  zipLookup,
		cache:      c,
		pointIndex: pointIndex,
	}, nil
}

func (b *LocationBackend) IsID(locationID string) bool {
	return IsID(locationID)
}

func (*LocationBackend) HasInventory(locationID string) bool {
	return false
}

func (b *LocationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	locationID = strings.TrimSpace(locationID)
	if !IsID(locationID) {
		return nil, fmt.Errorf("albertsons location id %q is invalid", locationID)
	}

	return b.locationByID(ctx, locationID)
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	requestedCentroid, ok := b.zipLookup.ZipCentroidByZIP(strings.TrimSpace(zipcode))
	if !ok {
		return nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, nil, nearby.MaxLocationDistanceMiles), nil
	}

	candidateIDs := b.candidateIDsForCentroid(requestedCentroid)
	candidates := make([]locationtypes.Location, 0, len(candidateIDs))
	for _, locationID := range candidateIDs {
		loc, err := b.locationByID(ctx, locationID)
		if err != nil {
			slog.WarnContext(ctx, "failed to load cached albertsons location for zip query", "location_id", locationID, "zip", zipcode, "error", err)
			continue
		}
		candidates = append(candidates, *loc)
	}

	return nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, candidates, nearby.MaxLocationDistanceMiles), nil
}

func (b *LocationBackend) locationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	summary, err := loadCachedStoreSummary(ctx, b.cache, locationID)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, fmt.Errorf("albertsons location %q not found", locationID)
		}
		return nil, fmt.Errorf("load albertsons location %q: %w", locationID, err)
	}
	b.applyPointFallback(summary)

	loc := storeSummaryToLocation(*summary)
	return &loc, nil
}

func (b *LocationBackend) candidateIDsForCentroid(requested locationtypes.ZipCentroid) []string {
	ids := make([]string, 0, len(b.pointIndex))
	for locationID, point := range b.pointIndex {
		distance := geo.HaversineMiles(requested.Lat, requested.Lon, point.Lat, point.Lon)
		if distance > nearby.MaxLocationDistanceMiles {
			continue
		}
		ids = append(ids, locationID)
	}

	sort.Strings(ids)
	return ids
}

func (b *LocationBackend) applyPointFallback(summary *StoreSummary) {
	if summary == nil || summary.ID == "" || (summary.Lat != nil && summary.Lon != nil) {
		return
	}

	point, ok := b.pointIndex[summary.ID]
	if !ok {
		return
	}
	lat := point.Lat
	lon := point.Lon
	summary.Lat = &lat
	summary.Lon = &lon
}
