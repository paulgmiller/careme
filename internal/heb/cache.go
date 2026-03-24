package heb

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
	Container              = "heb"
	StoreCachePrefix       = "heb/stores/"
	StoreURLMapCacheKey    = "heb/store_url_map.json"
	LocationIDPrefix       = "heb_"
	DefaultStoreSitemapURL = "https://www.heb.com/sitemap/storeSitemap.xml"
	LocationIndexCacheKey  = "heb/store_locations.json"
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
		return storeSummaryToIndexEntry(summary, zipLookup)
	})
	return err
}

func loadCachedStoreSummaryByID(ctx context.Context, c cache.Cache, locationID string) (*StoreSummary, error) {
	reader, err := c.Get(ctx, StoreCachePrefix+locationID)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return nil, fmt.Errorf("decode heb store summary: %w", err)
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

func storeSummaryToLocation(summary StoreSummary) locationtypes.Location {
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
