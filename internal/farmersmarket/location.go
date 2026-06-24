package farmersmarket

import (
	"context"
	"fmt"

	"careme/internal/cache"
	"careme/internal/locations/nearby"
	locationtypes "careme/internal/locations/types"
)

type ZipCentroidLookup interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type LocationBackend struct {
	identityProvider
	store     *Store
	zipLookup ZipCentroidLookup
}

func NewLocationBackend(store *Store, zipLookup ZipCentroidLookup) *LocationBackend {
	return &LocationBackend{store: store, zipLookup: zipLookup}
}

func NewContainerLocationBackend(zipLookup ZipCentroidLookup) (*LocationBackend, error) {
	store, err := NewContainerStore()
	if err != nil {
		return nil, err
	}
	return NewLocationBackend(store, zipLookup), nil
}

func (b *LocationBackend) HasInventory(locationID string) bool {
	return b != nil && b.store != nil && b.store.hasFreshInventory(locationID)
}

func (b *LocationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	if b == nil || b.store == nil {
		return nil, cache.ErrNotFound
	}
	market, err := b.store.loadMarket(ctx, locationID)
	if err != nil {
		return nil, err
	}
	loc := market.Location()
	return &loc, nil
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	if b == nil || b.store == nil {
		return nil, cache.ErrNotFound
	}
	if b.zipLookup == nil {
		return nil, fmt.Errorf("zip lookup is required")
	}
	markets, err := b.store.listMarkets(ctx)
	if err != nil {
		return nil, err
	}

	locations := make([]locationtypes.Location, 0, len(markets))
	for _, market := range markets {
		locations = append(locations, market.Location())
	}
	return nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, locations, nearby.MaxLocationDistanceMiles), nil
}
