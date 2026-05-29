package publix

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	if !cfg.Publix.IsEnabled() {
		return nil, locationtypes.DisabledBackendError("Publix")
	}

	if zipLookup == nil {
		return nil, fmt.Errorf("zip centroid lookup is required")
	}

	listCache, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create Publix list cache: %w", err)
	}

	backend, err := newLocationBackend(ctx, listCache, zipLookup)
	if err != nil {
		return nil, err
	}
	backend.hasInventory = hasInventory(ctx, cfg.Publix, listCache)
	return backend, nil
}

func hasInventory(ctx context.Context, cfg config.PublixConfig, c cache.Cache) bool {
	if cfg.HasInventory() {
		return true
	}
	if c == nil {
		return false
	}

	if _, err := LoadLatestAbck(ctx, c); err == nil {
		return true
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.WarnContext(ctx, "failed to read cached publix abck token for inventory status", "error", err)
	}
	return false
}

func newLocationBackend(ctx context.Context, c cache.Cache, zipLookup centroidByZip) (*LocationBackend, error) {
	entries, err := storeindex.Load(ctx, c, LocationIndexCacheKey)
	if err != nil {
		return nil, fmt.Errorf("load publix locations index: %w", err)
	}

	spatial := make([]locationtypes.Location, 0, len(entries))
	for _, entry := range entries {
		spatial = append(spatial, entry.ToLocation())
	}

	return &LocationBackend{
		zipLookup: zipLookup,
		spatial:   spatial,
		hydrator:  hydrator.NewLazyHydrator(&loader{c}),
	}, nil
}

func (b *LocationBackend) IsID(locationID string) bool {
	locationID = strings.TrimSpace(locationID)
	return strings.HasPrefix(locationID, LocationIDPrefix) && len(locationID) > len(LocationIDPrefix)
}

func (b *LocationBackend) HasInventory(locationID string) bool {
	return b.hasInventory
}

func (b *LocationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	locationID = strings.TrimSpace(locationID)
	if !b.IsID(locationID) {
		return nil, fmt.Errorf("publix location id %q is invalid", locationID)
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
