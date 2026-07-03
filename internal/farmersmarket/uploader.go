package farmersmarket

import (
	"context"
	"fmt"
	"time"

	"careme/internal/ai"
	"careme/internal/locations/geo"
)

type uploader struct {
	store *store
}

// private for tests
func NewUploader(store *store) *uploader {
	if store == nil {
		panic("store is required")
	}
	return &uploader{store: store}
}

func NewContainerUploader() (*uploader, error) {
	store, err := NewContainerStore()
	if err != nil {
		return nil, err
	}
	return NewUploader(store), nil
}

// create or return a market and merge its inventory into cacheresolveMarketLocation
func (u *uploader) saveUpload(ctx context.Context, name string, coor geo.Coordinate, zip string,
	photoCount int, date time.Time, ingredients []ai.InputIngredient,
) (*Market, error) {
	if photoCount <= 0 {
		return nil, fmt.Errorf("at least one market photo is required")
	}
	if err := coor.Valid(); err != nil {
		return nil, fmt.Errorf("invalid market coordinates: %w", err)
	}

	market, err := u.store.findNearbyMarket(ctx, coor.Lat, coor.Lon)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if market == nil {
		market = &Market{
			Coordinate: coor,
			ID:         marketID(name, coor.Lat, coor.Lon),
			Names:      []string{name},
			ZipCode:    zip,
			PhotoCount: photoCount,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	} else {
		market.merge(name, coor.Lat, coor.Lon, photoCount, now)
		if market.ZipCode == "" {
			market.ZipCode = zip
		}
	}

	if err := u.store.saveMarket(ctx, *market); err != nil {
		return nil, err
	}

	if err := u.store.mergeInventory(ctx, market.ID, date, ingredients); err != nil {
		return nil, err
	}
	return market, nil
}
