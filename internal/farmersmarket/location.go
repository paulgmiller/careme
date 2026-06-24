package farmersmarket

import (
	"context"

	"careme/internal/locations/nearby"
	locationtypes "careme/internal/locations/types"

	"github.com/samber/lo"
)

type ZipCentroidLookup interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type locationBackend struct {
	identityProvider
	store     *Store
	zipLookup ZipCentroidLookup
}

func NewLocationBackend(store *Store, zipLookup ZipCentroidLookup) *locationBackend {
	if store == nil {
		panic("nil store given to location backend")
	}
	if zipLookup == nil {
		panic("nil zip lookup given to location backend")
	}
	return &locationBackend{store: store, zipLookup: zipLookup}
}

func NewContainerLocationBackend(zipLookup ZipCentroidLookup) (*locationBackend, error) {
	store, err := NewContainerStore()
	if err != nil {
		return nil, err
	}
	return NewLocationBackend(store, zipLookup), nil
}

func (b *locationBackend) HasInventory(locationID string) bool {
	return b.store.hasFreshInventory(locationID)
}

func (b *locationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	market, err := b.store.loadMarket(ctx, locationID)
	if err != nil {
		return nil, err
	}
	loc := market.Location()
	return &loc, nil
}

func (b *locationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	markets, err := b.store.listMarkets(ctx)
	if err != nil {
		return nil, err
	}

	locations := lo.Map(markets, func(market Market, _ int) locationtypes.Location {
		return market.Location()
	})
	return nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, locations, nearby.MaxLocationDistanceMiles), nil
}
