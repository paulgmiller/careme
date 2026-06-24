package farmersmarket

import (
	"context"
	"fmt"
	"time"

	"careme/internal/ai"
	"careme/internal/locations/geo"
)

type ZipFinder interface {
	NearestZIPToCoordinates(lat, lon float64) (string, bool)
}

type Uploader struct {
	store     *store
	zipFinder ZipFinder
}

// private for tests
func NewUploader(store *store, zipFinder ZipFinder) *Uploader {
	if store == nil {
		panic("store is required")
	}
	if zipFinder == nil {
		panic("zip finder is required")
	}
	return &Uploader{store: store, zipFinder: zipFinder}
}

func NewContainerUploader(zipFinder ZipFinder) (*Uploader, error) {
	store, err := NewContainerStore()
	if err != nil {
		return nil, err
	}
	return NewUploader(store, zipFinder), nil
}

func (u *Uploader) SaveUpload(ctx context.Context, name string, lat, lon float64, photoCount int, date time.Time, ingredients []ai.InputIngredient) (*Market, []ai.InputIngredient, error) {
	if photoCount <= 0 {
		return nil, nil, fmt.Errorf("at least one geotagged photo is required")
	}
	coor := geo.Coordinate{Lat: lat, Lon: lon}
	if !coor.Valid() {
		return nil, nil, fmt.Errorf("invalid market coordinates")
	}

	market, err := u.store.findNearbyMarket(ctx, lat, lon)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	if market == nil {
		zip, _ := u.zipFinder.NearestZIPToCoordinates(lat, lon)
		market = &Market{
			Coordinate: coor,
			ID:         marketID(name, lat, lon),
			Names:      []string{name},
			ZipCode:    zip,
			PhotoCount: photoCount,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	} else {
		market.merge(name, lat, lon, photoCount, now)
		if market.ZipCode == "" {
			market.ZipCode, _ = u.zipFinder.NearestZIPToCoordinates(market.Lat, market.Lon)
		}
	}

	if err := u.store.saveMarket(ctx, *market); err != nil {
		return nil, nil, err
	}

	merged, err := u.store.mergeInventory(ctx, market.ID, date, ingredients)
	if err != nil {
		return nil, nil, err
	}
	return market, merged, nil
}
