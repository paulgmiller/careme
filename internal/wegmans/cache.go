package wegmans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"careme/internal/cache"
	"careme/internal/locations/storeindex"

	locationtypes "careme/internal/locations/types"
)

const LocationIndexCacheKey = "wegmans/store_locations.json"

func CacheStoreSummary(ctx context.Context, c cache.Cache, summary *StoreSummary) error {
	if summary == nil {
		return errors.New("store summary is required")
	}
	if summary.StoreNumber == 0 {
		return errors.New("store summary store number is required")
	}

	raw, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal store summary: %w", err)
	}

	if err := c.Put(ctx, StoreCachePrefix+strconv.Itoa(summary.StoreNumber), string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write store summary cache: %w", err)
	}
	return nil
}

func RebuildLocationIndex(ctx context.Context, c cache.ListCache, zipLookup storeindex.ZipCentroidLookup) error {
	_, err := storeindex.RebuildFromStoreSummaries[StoreSummary](ctx, c, StoreCachePrefix, LocationIndexCacheKey,
		func(summary StoreSummary) storeindex.Entry {
			return storeSummaryToIndexEntry(summary, zipLookup)
		})
	return err
}

type loader struct {
	cache cache.Cache
}

func (l *loader) Load(ctx context.Context, locationID string) (locationtypes.Location, error) {
	storeNumber := strings.TrimPrefix(strings.TrimSpace(locationID), LocationIDPrefix)
	if storeNumber == "" {
		return locationtypes.Location{}, fmt.Errorf("wegmans location %q not found", locationID)
	}

	reader, err := l.cache.Get(ctx, StoreCachePrefix+storeNumber)
	if err != nil {
		return locationtypes.Location{}, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return locationtypes.Location{}, fmt.Errorf("decode wegmans store summary: %w", err)
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

func storeSummaryToIndexEntry(summary StoreSummary, zipLookup storeindex.ZipCentroidLookup) storeindex.Entry {
	lat, lon := storeindex.Coordinates(summary.Lat, summary.Lon, summary.ZipCode, zipLookup)
	return storeindex.Entry{
		ID:  summary.ID,
		Lat: lat,
		Lon: lon,
	}
}
