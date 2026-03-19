package albertsons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"careme/internal/cache"
	"careme/internal/locations/pointindex"
	locationtypes "careme/internal/locations/types"
	"careme/internal/sitemapfetch"

	"github.com/samber/lo"
	lop "github.com/samber/lo/parallel"
)

const (
	Container               = "albertsons"
	StoreCachePrefix        = "albertsons/stores/"
	StoreURLMapCacheKey     = "albertsons/store_url_map.json"
	StorePointIndexCacheKey = "albertsons/store_points.json"
)

type StorePoint = pointindex.Point

func SaveStoreURLMap(ctx context.Context, c cache.Cache, urlMap map[string]string) error {
	return sitemapfetch.SaveURLMap(ctx, c, StoreURLMapCacheKey, urlMap)
}

func LoadStoreURLMap(ctx context.Context, c cache.Cache) (map[string]string, error) {
	return sitemapfetch.LoadURLMap(ctx, c, StoreURLMapCacheKey)
}

func SaveStorePointIndex(ctx context.Context, c cache.Cache, pointIndex map[string]StorePoint) error {
	return pointindex.Save(ctx, c, StorePointIndexCacheKey, pointIndex)
}

func LoadStorePointIndex(ctx context.Context, c cache.Cache) (map[string]StorePoint, error) {
	return pointindex.Load(ctx, c, StorePointIndexCacheKey)
}

func LoadOrBuildStorePointIndex(ctx context.Context, c cache.ListCache) (map[string]StorePoint, error) {
	return pointindex.LoadOrBuild(ctx, c, StorePointIndexCacheKey, StoreCachePrefix, func(ctx context.Context, c cache.ListCache) ([]locationtypes.Location, error) {
		summaries, err := loadCachedStoreSummaries(ctx, c)
		if err != nil {
			return nil, err
		}

		locations := make([]locationtypes.Location, 0, len(summaries))
		for _, summary := range summaries {
			locations = append(locations, storeSummaryToLocation(*summary))
		}
		return locations, nil
	})
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

func loadCachedStoreSummaries(ctx context.Context, c cache.ListCache) ([]*StoreSummary, error) {
	keys, err := c.List(ctx, StoreCachePrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list cached store summaries: %w", err)
	}

	// expensive. Just save a smaller map of centroids
	summaries := lop.Map(keys, func(key string, _ int) *StoreSummary {
		reader, err := c.Get(ctx, StoreCachePrefix+key)
		if err != nil {
			slog.WarnContext(ctx, "failed to read cached albertsons store summary", "key", key, "error", err)
			return nil
		}
		defer func() {
			_ = reader.Close()
		}()

		var summary StoreSummary
		if err := json.NewDecoder(reader).Decode(&summary); err != nil {
			slog.WarnContext(ctx, "failed to decode cached albertsons store summary", "key", key, "error", err)
			return nil
		}
		return &summary
	})

	summaries = lo.Compact(summaries)
	if len(summaries) == 0 {
		return nil, fmt.Errorf("failed to load albertsons locations")
	}
	slog.InfoContext(ctx, "loaded albertsons locations", "count", len(summaries))

	return summaries, nil
}

func loadCachedStoreSummary(ctx context.Context, c cache.Cache, locationID string) (*StoreSummary, error) {
	reader, err := c.Get(ctx, StoreCachePrefix+locationID)
	if err != nil {
		return nil, fmt.Errorf("read cached albertsons store summary: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return nil, fmt.Errorf("decode cached albertsons store summary: %w", err)
	}
	return &summary, nil
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
