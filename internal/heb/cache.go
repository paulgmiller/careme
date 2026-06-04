package heb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"careme/internal/cache"

	locationtypes "careme/internal/locations/types"
)

const (
	Container        = "heb"
	StoreCachePrefix = "heb/stores/"
	LocationIDPrefix = "heb_"
)

func CacheStoreSummary(ctx context.Context, c cache.Cache, summary *StoreSummary) error {
	if summary == nil {
		return errors.New("store summary is required")
	}

	raw, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal store summary: %w", err)
	}

	if err := c.Put(ctx, StoreCachePrefix+summary.ID, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write store summary cache: %w", err)
	}
	return nil
}

type loader struct {
	cache cache.Cache
}

func (l *loader) Load(ctx context.Context, locationID string) (locationtypes.Location, error) {
	reader, err := l.cache.Get(ctx, StoreCachePrefix+locationID)
	if err != nil {
		return locationtypes.Location{}, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return locationtypes.Location{}, fmt.Errorf("decode heb store summary: %w", err)
	}
	return locationtypes.Location{
		ID:      summary.ID,
		Name:    summary.Name,
		Address: summary.Address,
		State:   summary.State,
		ZipCode: summary.ZipCode,
		Lat:     summary.Lat,
		Lon:     summary.Lon,
		Chain:   Container,
	}, nil
}
