package farmersmarket

import (
	"context"
	"fmt"
	"strings"
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

// saveUpload creates or updates a market and merges its inventory into the cache.
func (u *uploader) saveUpload(ctx context.Context, name string, coor geo.Coordinate, timezoneOrZIP string,
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
	timezone := timezoneOrZIP
	zip := ""
	if !strings.Contains(timezoneOrZIP, "/") {
		timezone = ""
		zip = timezoneOrZIP
	}
	if market == nil {
		market = &Market{
			Coordinate: coor,
			ID:         marketID(name, coor.Lat, coor.Lon),
			Names:      []string{name},
			ZipCode:    zip,
			Timezone:   timezone,
			PhotoCount: photoCount,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	} else {
		market.merge(name, coor.Lat, coor.Lon, photoCount, now)
		if market.ZipCode == "" {
			market.ZipCode = zip
		}
		if market.Timezone == "" {
			market.Timezone = timezone
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
