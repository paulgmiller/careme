package heb

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations/nearby"
	"careme/internal/locations/storeindex"
	"context"
	"fmt"
	"strings"
	"sync"

	locationtypes "careme/internal/locations/types"
)

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type LocationBackend struct {
	zipLookup    centroidByZip
	storeCache   cache.Cache
	spatial      []locationtypes.Location
	hydratedByID map[string]locationtypes.Location
	mu           sync.RWMutex
}

func NewLocationBackendFromConfig(ctx context.Context, cfg *config.Config, zipLookup centroidByZip) (*LocationBackend, error) {
	if !cfg.HEB.IsEnabled() {
		return nil, locationtypes.DisabledBackendError("HEB")
	}

	if zipLookup == nil {
		return nil, fmt.Errorf("zip centroid lookup is required")
	}

	listCache, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create HEB list cache: %w", err)
	}

	return newLocationBackend(ctx, listCache, zipLookup)
}

func newLocationBackend(ctx context.Context, c cache.Cache, zipLookup centroidByZip) (*LocationBackend, error) {
	if c == nil {
		return nil, fmt.Errorf("cache is required")
	}

	entries, err := storeindex.Load(ctx, c, LocationIndexCacheKey)
	if err != nil {
		return nil, fmt.Errorf("load heb locations index: %w", err)
	}

	spatial := make([]locationtypes.Location, 0, len(entries))
	for _, entry := range entries {
		spatial = append(spatial, entry.ToLocation())
	}

	return &LocationBackend{
		zipLookup:    zipLookup,
		storeCache:   c,
		spatial:      spatial,
		hydratedByID: make(map[string]locationtypes.Location),
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
		return nil, fmt.Errorf("heb location id %q is invalid", locationID)
	}

	loc, err := b.hydrateLocation(ctx, locationID)
	if err != nil {
		return nil, err
	}
	copy := loc
	return &copy, nil
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	candidates := nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, b.spatial, nearby.MaxLocationDistanceMiles)
	return storeindex.HydrateLocations(ctx, candidates, b.hydrateLocation)
}

func (b *LocationBackend) hydrateLocation(ctx context.Context, locationID string) (locationtypes.Location, error) {
	b.mu.RLock()
	loc, ok := b.hydratedByID[locationID]
	b.mu.RUnlock()
	if ok {
		return loc, nil
	}

	summary, err := loadCachedStoreSummaryByID(ctx, b.storeCache, locationID)
	if err != nil {
		return locationtypes.Location{}, fmt.Errorf("heb location %q not found: %w", locationID, err)
	}
	loc = storeSummaryToLocation(*summary)

	b.mu.Lock()
	b.hydratedByID[locationID] = loc
	b.mu.Unlock()
	return loc, nil
}
