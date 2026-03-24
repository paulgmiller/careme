package albertsons

import (
	"careme/internal/cache"
	"careme/internal/locations/storeindex"
	"careme/internal/sitemapfetch"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	locationtypes "careme/internal/locations/types"
)

const (
	Container             = "albertsons"
	StoreCachePrefix      = "albertsons/stores/"
	StoreURLMapCacheKey   = "albertsons/store_url_map.json"
	LocationIndexCacheKey = "albertsons/store_locations.json"
)

func SaveStoreURLMap(ctx context.Context, c cache.Cache, urlMap map[string]string) error {
	return sitemapfetch.SaveURLMap(ctx, c, StoreURLMapCacheKey, urlMap)
}

func LoadStoreURLMap(ctx context.Context, c cache.Cache) (map[string]string, error) {
	return sitemapfetch.LoadURLMap(ctx, c, StoreURLMapCacheKey)
}

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

func RebuildLocationIndex(ctx context.Context, c cache.ListCache, zipLookup storeindex.ZipCentroidLookup) error {
	_, err := storeindex.RebuildFromStoreSummaries(ctx, c, StoreCachePrefix, LocationIndexCacheKey, func(summary StoreSummary) storeindex.Entry {
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
	reader, err := l.cache.Get(ctx, StoreCachePrefix+locationID)
	if err != nil {
		return locationtypes.Location{}, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return locationtypes.Location{}, fmt.Errorf("decode albertsons store summary: %w", err)
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
