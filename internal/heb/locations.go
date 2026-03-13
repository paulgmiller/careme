package heb

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations/nearby"
	locationtypes "careme/internal/locations/types"
	"context"
	"fmt"
	"log/slog"
	"strings"
)

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type LocationBackend struct {
	zipLookup centroidByZip
	byID      map[string]locationtypes.Location
}

func NewLocationBackendFromConfig(ctx context.Context, cfg *config.Config, zipLookup centroidByZip) (*LocationBackend, error) {
	if !cfg.HEB.IsEnabled() {
		return nil, &locationtypes.DisabledBackendError{Backend: "HEB"}
	}

	slog.Info("initializing HEB location backend")

	listCache, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create HEB list cache: %w", err)
	}

	backend, err := NewLocationBackend(ctx, listCache, zipLookup)
	if err != nil {
		return nil, fmt.Errorf("create HEB backend: %w", err)
	}

	return backend, nil
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
		return nil, fmt.Errorf("heb location id %q is invalid", locationID)
	}

	loc, exists := b.byID[locationID]
	if !exists {
		return nil, fmt.Errorf("heb location %q not found", locationID)
	}

	copy := loc
	return &copy, nil
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	candidates := make([]locationtypes.Location, 0, len(b.byID))
	for _, loc := range b.byID {
		candidates = append(candidates, loc)
	}
	return nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, candidates, nearby.MaxLocationDistanceMiles), nil
}
