package wegmans

import (
	"careme/internal/cache"
	"careme/internal/locations/storeindex"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

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

func loadCachedStoreSummaryByID(ctx context.Context, c cache.Cache, locationID string) (*StoreSummary, error) {
	storeNumber := strings.TrimPrefix(strings.TrimSpace(locationID), LocationIDPrefix)
	if storeNumber == "" {
		return nil, fmt.Errorf("wegmans location %q not found", locationID)
	}

	reader, err := c.Get(ctx, StoreCachePrefix+storeNumber)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return nil, fmt.Errorf("decode wegmans store summary: %w", err)
	}
	return &summary, nil
}

func storeSummaryToIndexEntry(summary StoreSummary, zipLookup storeindex.ZipCentroidLookup) storeindex.Entry {
	lat, lon := storeindex.Coordinates(summary.Lat, summary.Lon, summary.ZipCode, zipLookup)
	return storeindex.Entry{
		ID:  summary.ID,
		Lat: lat,
		Lon: lon,
	}
}

func StoreSummaryToLocation(summary StoreSummary) locationtypes.Location {
	return locationtypes.Location{
		ID:      summary.ID,
		Name:    summary.Name,
		Address: summary.Address,
		State:   summary.State,
		ZipCode: summary.ZipCode,
		Lat:     summary.Lat,
		Lon:     summary.Lon,
		Chain:   Container,
	}
}
