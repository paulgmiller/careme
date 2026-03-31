package albertsons

import (
	"context"
	"fmt"
	"strings"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations/hydrator"
	"careme/internal/locations/nearby"
	"careme/internal/locations/storeindex"

	locationtypes "careme/internal/locations/types"
)

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type LocationBackend struct {
	zipLookup    centroidByZip
	spatial      []locationtypes.Location
	hydrator     *hydrator.LazyHydrator
	hasInventory bool
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

	return newLocationBackend(ctx, listCache, zipLookup, true /*hasInventory*/)
}

func newLocationBackend(ctx context.Context, c cache.ListCache, zipLookup centroidByZip, inventory bool) (*LocationBackend, error) {
	entries, err := storeindex.Load(ctx, c, LocationIndexCacheKey)
	if err != nil {
		return nil, fmt.Errorf("load albertsons locations index: %w", err)
	}

	spatial := make([]locationtypes.Location, 0, len(entries))
	for _, entry := range entries {
		spatial = append(spatial, entry.ToLocation())
	}

	return &LocationBackend{
		zipLookup:    zipLookup,
		spatial:      spatial,
		hydrator:     hydrator.NewLazyHydrator(&loader{c}),
		hasInventory: inventory,
	}, nil
}

func (b *LocationBackend) IsID(locationID string) bool {
	return IsID(locationID)
}

func (l *LocationBackend) HasInventory(locationID string) bool {
	// do we want to make this dynamic
	return l.hasInventory
}

func (b *LocationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	locationID = strings.TrimSpace(locationID)
	if !IsID(locationID) {
		return nil, fmt.Errorf("albertsons location id %q is invalid", locationID)
	}

	loc, err := b.hydrator.Hydrate(ctx, locationID)
	if err != nil {
		return nil, err
	}
	copy := loc
	return &copy, nil
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	candidates := nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, b.spatial, nearby.MaxLocationDistanceMiles)
	return storeindex.HydrateLocations(ctx, candidates, b.hydrator.Hydrate)
}
