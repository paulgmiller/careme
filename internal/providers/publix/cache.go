package publix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"careme/internal/cache"
	"careme/internal/locations/storeindex"

	locationtypes "careme/internal/locations/types"
)

const (
	Container               = "publix"
	StoreCachePrefix        = "publix/stores/"
	StoreURLMapCacheKey     = "publix/store_url_map.json"
	MissingStoreIDsCacheKey = "publix/missing_store_ids.json"
	LocationIDPrefix        = "publix_"
	LocationIndexCacheKey   = "publix/store_locations.json"
)

func SaveMissingStoreIDs(ctx context.Context, c cache.Cache, ids map[string]struct{}) error {
	stored := make([]string, 0, len(ids))
	for id := range ids {
		if id == "" {
			continue
		}
		stored = append(stored, id)
	}
	slices.Sort(stored)

	raw, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("marshal missing store ids: %w", err)
	}
	if err := c.Put(ctx, MissingStoreIDsCacheKey, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write missing store ids cache: %w", err)
	}
	return nil
}

func LoadMissingStoreIDs(ctx context.Context, c cache.Cache) (map[string]struct{}, error) {
	reader, err := c.Get(ctx, MissingStoreIDsCacheKey)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var stored []string
	if err := json.NewDecoder(reader).Decode(&stored); err != nil {
		return nil, fmt.Errorf("decode missing store ids cache: %w", err)
	}

	ids := make(map[string]struct{}, len(stored))
	for _, id := range stored {
		if id == "" {
			continue
		}
		ids[id] = struct{}{}
	}
	return ids, nil
}

func CacheStoreSummary(ctx context.Context, c cache.Cache, summary *StoreSummary) error {
	if summary == nil {
		return errors.New("store summary is required")
	}
	if summary.StoreID == "" {
		return errors.New("store summary store id is required")
	}

	raw, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal store summary: %w", err)
	}

	if err := c.Put(ctx, StoreCachePrefix+summary.StoreID, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write store summary cache: %w", err)
	}
	return nil
}

func RebuildLocationIndex(ctx context.Context, c cache.ListCache, zipLookup storeindex.ZipCentroidLookup) error {
	_, err := storeindex.RebuildFromStoreSummaries(ctx, c, StoreCachePrefix, LocationIndexCacheKey,
		func(summary StoreSummary) storeindex.Entry {
			lat, lon := storeindex.Coordinates(summary.Lat, summary.Lon, summary.ZipCode, zipLookup)
			return storeindex.Entry{
				ID:  summary.ID,
				Lat: lat,
				Lon: lon,
			}
		})
	return err
}

type loader struct {
	cache cache.Cache
}

func (l *loader) Load(ctx context.Context, locationID string) (locationtypes.Location, error) {
	storeID := strings.TrimPrefix(strings.TrimSpace(locationID), LocationIDPrefix)
	if storeID == "" {
		return locationtypes.Location{}, fmt.Errorf("publix location %q not found", locationID)
	}

	reader, err := l.cache.Get(ctx, StoreCachePrefix+storeID)
	if err != nil {
		return locationtypes.Location{}, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return locationtypes.Location{}, fmt.Errorf("decode publix store summary: %w", err)
	}
	return locationtypes.Location{
		ID:      summary.ID,
		Name:    summary.Name,
		Address: summary.Address,
		State:   summary.State,
		ZipCode: summary.ZipCode,
		Lat:     summary.Lat,
		Lon:     summary.Lon,
		Chain:   "publix",
	}, nil
}
