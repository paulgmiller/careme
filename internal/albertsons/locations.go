package albertsons

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations/geo"
	"careme/internal/locations/nearby"
	"careme/internal/locations/pointindex"
	"careme/internal/parallelism"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	locationtypes "careme/internal/locations/types"
)

type centroidByZip = ZIPCentroidLookup

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
	pointIndex, err := pointindex.LoadOrBuild(ctx, c, zipLookup, LoadCachedStoreSummaries)
	if err != nil {
		return nil, err
	}
	// should we alert if this is ever zero or just get uptime robot ping on location?
	// or a readiness probe thats only checked in prod
	slog.InfoContext(ctx, "loaded pointmap for albertsons", "count", len(pointIndex))

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
		return nil, fmt.Errorf("requested zip %s has no centroid", zipcode)
	}

	candidateIDs := b.candidateIDsForCentroid(requestedCentroid)
	candidates, err := parallelism.MapWithError(candidateIDs, func(locationID string) (locationtypes.Location, error) {
		l, err := b.locationByID(ctx, locationID)
		return *l, err
	})
	if err != nil {
		if len(candidates) == 0 {
			return nil, fmt.Errorf("failed to load any locations for zip %s: %w", zipcode, err)
		}
		slog.WarnContext(ctx, "failed to load some albertsons locations for zip, returning partial results", "zip", zipcode, "error", err, "requested_centroid", requestedCentroid, "candidate_ids", candidateIDs)
	}

	return nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, candidates, nearby.MaxLocationDistanceMiles)
}

func (b *LocationBackend) locationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	summary, err := loadCachedStoreSummary(ctx, b.cache, locationID)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, fmt.Errorf("albertsons location %q not found", locationID)
		}
		return nil, fmt.Errorf("load albertsons location %q: %w", locationID, err)
	}

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

	return ids
}
