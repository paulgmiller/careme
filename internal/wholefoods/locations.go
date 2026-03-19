package wholefoods

import (
	"context"
	"fmt"
	"strings"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations/nearby"

	locationtypes "careme/internal/locations/types"
)

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type LocationBackend struct {
	zipLookup centroidByZip
	byID      map[string]locationtypes.Location
}

func NewLocationBackendFromConfig(ctx context.Context, cfg *config.Config, zipLookup centroidByZip) (*LocationBackend, error) {
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

func newLocationBackend(ctx context.Context, c cache.ListCache, zipLookup centroidByZip) (*LocationBackend, error) {
	// Is this too much? should we just fetch a single blob that is all coordinates -> store ids and lazily fetch stores?
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
	_, ok := parseLocationID(locationID)
	return ok
}

func (*LocationBackend) HasInventory(locationID string) bool {
	return true
}

func (b *LocationBackend) GetLocationByID(_ context.Context, locationID string) (*locationtypes.Location, error) {
	normalized, ok := parseLocationID(locationID)
	if !ok {
		return nil, fmt.Errorf("whole foods location id %q is invalid", locationID)
	}

	loc, exists := b.byID[normalized]
	if !exists {
		return nil, fmt.Errorf("whole foods location %q not found", locationID)
	}

	copy := loc
	return &copy, nil
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	candidates := make([]locationtypes.Location, 0, len(b.byID))
	for _, loc := range b.byID {
		candidates = append(candidates, loc)
	}
	return nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, candidates, nearby.MaxLocationDistanceMiles)
}

func parseLocationID(locationID string) (string, bool) {
	if !strings.HasPrefix(locationID, LocationIDPrefix) {
		return "", false
	}

	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	return LocationIDPrefix + storeID, true
}
