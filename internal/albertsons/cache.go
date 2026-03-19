package albertsons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"careme/internal/cache"
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

type StorePoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func SaveStoreURLMap(ctx context.Context, c cache.Cache, urlMap map[string]string) error {
	return sitemapfetch.SaveURLMap(ctx, c, StoreURLMapCacheKey, urlMap)
}

func LoadStoreURLMap(ctx context.Context, c cache.Cache) (map[string]string, error) {
	return sitemapfetch.LoadURLMap(ctx, c, StoreURLMapCacheKey)
}

func SaveStorePointIndex(ctx context.Context, c cache.Cache, pointIndex map[string]StorePoint) error {
	if pointIndex == nil {
		pointIndex = map[string]StorePoint{}
	}

	raw, err := json.Marshal(pointIndex)
	if err != nil {
		return fmt.Errorf("marshal store point index: %w", err)
	}

	if err := c.Put(ctx, StorePointIndexCacheKey, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write store point index cache: %w", err)
	}
	return nil
}

func LoadStorePointIndex(ctx context.Context, c cache.Cache) (map[string]StorePoint, error) {
	reader, err := c.Get(ctx, StorePointIndexCacheKey)
	if err != nil {
		return nil, fmt.Errorf("read store point index cache: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	pointIndex := make(map[string]StorePoint)
	if err := json.NewDecoder(reader).Decode(&pointIndex); err != nil {
		return nil, fmt.Errorf("decode store point index cache: %w", err)
	}
	return pointIndex, nil
}

func LoadOrBuildStorePointIndex(ctx context.Context, c cache.ListCache) (map[string]StorePoint, error) {
	pointIndex, err := LoadStorePointIndex(ctx, c)
	if err == nil {
		return pointIndex, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return nil, err
	}

	keys, err := c.List(ctx, StoreCachePrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list cached store summaries: %w", err)
	}
	if len(keys) == 0 {
		return map[string]StorePoint{}, nil
	}

	summaries, err := loadCachedStoreSummaries(ctx, c)
	if err != nil {
		return nil, err
	}

	pointIndex = buildStorePointIndex(summaries)
	if err := SaveStorePointIndex(ctx, c, pointIndex); err != nil {
		return nil, err
	}
	return pointIndex, nil
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

func buildStorePointIndex(summaries []*StoreSummary) map[string]StorePoint {
	pointIndex := make(map[string]StorePoint, len(summaries))
	for _, summary := range summaries {
		if summary == nil || summary.ID == "" || summary.Lat == nil || summary.Lon == nil {
			continue
		}
		pointIndex[summary.ID] = StorePoint{
			Lat: *summary.Lat,
			Lon: *summary.Lon,
		}
	}
	return pointIndex
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
