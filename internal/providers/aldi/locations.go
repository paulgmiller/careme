package aldi

import (
	"context"
	"fmt"
	"strings"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations/geo"
	"careme/internal/locations/hydrator"
	"careme/internal/locations/nearby"
	"careme/internal/locations/storeindex"

	locationtypes "careme/internal/locations/types"
)

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type locationBackend struct {
	zipLookup centroidByZip
	spatial   []locationtypes.Location
	hydrator  *hydrator.LazyHydrator
}

func NewLocationBackendFromConfig(ctx context.Context, cfg *config.Config, zipLookup centroidByZip) (*locationBackend, error) {
	if !cfg.Aldi.IsEnabled() {
		return nil, locationtypes.DisabledBackendError("ALDI")
	}
	if zipLookup == nil {
		return nil, fmt.Errorf("zip centroid lookup is required")
	}

	listCache, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create ALDI list cache: %w", err)
	}
	return newLocationBackend(ctx, listCache, zipLookup)
}

func newLocationBackend(ctx context.Context, c cache.Cache, zipLookup centroidByZip) (*locationBackend, error) {
	entries, err := storeindex.Load(ctx, c, LocationIndexCacheKey)
	if err != nil {
		return nil, fmt.Errorf("load aldi locations index: %w", err)
	}

	spatial := make([]locationtypes.Location, 0, len(entries))
	for _, entry := range entries {
		spatial = append(spatial, entry.ToLocation())
	}

	return &locationBackend{
		zipLookup: zipLookup,
		spatial:   spatial,
		hydrator:  hydrator.NewLazyHydrator(&loader{c}),
	}, nil
}

func (b *locationBackend) IsID(locationID string) bool {
	return isID(locationID)
}

func (b *locationBackend) HasInventory(locationID string) bool {
	// misisng summary ids already filtered
	return true
}

func (b *locationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	locationID = strings.TrimSpace(locationID)
	if !isID(locationID) {
		return nil, fmt.Errorf("ALDI location id %q is invalid", locationID)
	}

	loc, err := b.hydrator.Hydrate(ctx, locationID)
	if err != nil {
		return nil, err
	}
	copy := loc
	return &copy, nil
}

func (b *locationBackend) GetLocationsByCoordinates(ctx context.Context, coordinates geo.Coordinate) ([]locationtypes.Location, error) {
	candidates := nearby.FilterAndSortByCoordinates(coordinates, b.spatial, nearby.MaxLocationDistanceMiles)
	return storeindex.HydrateLocations(ctx, candidates, b.hydrator.Hydrate)
}

func isID(locationID string) bool {
	locationID = strings.TrimSpace(locationID)
	return strings.HasPrefix(locationID, locationIDPrefix) && strings.TrimPrefix(locationID, locationIDPrefix) != ""
}
