package farmersmarket

import (
	"context"
	"fmt"
	"time"

	"careme/internal/ai"
)

type ZipFinder interface {
	NearestZIPToCoordinates(lat, lon float64) (string, bool)
}

type Uploader struct {
	store     *Store
	zipFinder ZipFinder
}

func NewUploader(store *Store, zipFinder ZipFinder) *Uploader {
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
	if u == nil || u.store == nil || u.store.cache == nil {
		return nil, nil, fmt.Errorf("cache is required")
	}
	if u.zipFinder == nil {
		return nil, nil, fmt.Errorf("zip finder is required")
	}
	if photoCount <= 0 {
		return nil, nil, fmt.Errorf("at least one geotagged photo is required")
	}
	name = normalizeName(name)
	if name == "" {
		return nil, nil, fmt.Errorf("market name is required")
	}
	if !validCoordinate(lat, lon) {
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
			ID:         marketID(name, lat, lon),
			Names:      []string{name},
			Lat:        lat,
			Lon:        lon,
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
