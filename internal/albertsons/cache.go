package albertsons

import (
	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/samber/lo"
	lop "github.com/samber/lo/parallel"
)

const (
	Container           = "albertsons"
	StoreCachePrefix    = "albertsons/stores/"
	StoreURLMapCacheKey = "albertsons/store_url_map.json"
)

type StoreReference struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func SaveStoreURLMap(ctx context.Context, c cache.Cache, urlMap map[string]string) error {
	raw, err := json.Marshal(urlMap)
	if err != nil {
		return fmt.Errorf("marshal store url map: %w", err)
	}
	if err := c.Put(ctx, StoreURLMapCacheKey, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write store url map cache: %w", err)
	}
	return nil
}

func LoadStoreURLMap(ctx context.Context, c cache.Cache) (map[string]string, error) {
	reader, err := c.Get(ctx, StoreURLMapCacheKey)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var urlMap map[string]string
	if err := json.NewDecoder(reader).Decode(&urlMap); err != nil {
		return nil, fmt.Errorf("decode store url map cache: %w", err)
	}
	return urlMap, nil
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

	//expensive. Just save a smaller map of centroids
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

func storeSummaryToLocation(summary StoreSummary) locationtypes.Location {
	return locationtypes.Location{
		ID:      summary.ID,
		Name:    summary.Name,
		Address: summary.Address,
		State:   summary.State,
		ZipCode: summary.ZipCode,
		Lat:     summary.Lat,
		Lon:     summary.Lon,
	}
}
