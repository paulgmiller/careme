package wholefoods

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
	if !cfg.WholeFoods.IsEnabled() {
		return nil, locationtypes.DisabledBackendError("Whole Foods")
	}

	if zipLookup == nil {
		return nil, fmt.Errorf("zip centroid lookup is required")
	}

	listCache, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create Whole Foods list cache: %w", err)
	}

	return newLocationBackend(ctx, listCache, zipLookup)
}

func newLocationBackend(ctx context.Context, c cache.Cache, zipLookup centroidByZip) (*locationBackend, error) {
	entries, err := storeindex.Load(ctx, c, locationIndexCacheKey)
	if err != nil {
		return nil, fmt.Errorf("load wholefoods locations index: %w", err)
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
	_, ok := parseLocationID(locationID)
	return ok
}

func (*locationBackend) HasInventory(locationID string) bool {
	return true
}

func (b *locationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	normalized, ok := parseLocationID(locationID)
	if !ok {
		return nil, fmt.Errorf("whole foods location id %q is invalid", locationID)
	}

	loc, err := b.hydrator.Hydrate(ctx, normalized)
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

func parseLocationID(locationID string) (string, bool) {
	if !strings.HasPrefix(locationID, locationIDPrefix) {
		return "", false
	}

	storeID := strings.TrimPrefix(locationID, locationIDPrefix)
	return locationIDPrefix + storeID, true
}
