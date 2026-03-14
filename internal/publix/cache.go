package publix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"

	"github.com/samber/lo"
	lop "github.com/samber/lo/parallel"
)

const (
	Container               = "publix"
	StoreCachePrefix        = "publix/stores/"
	StoreURLMapCacheKey     = "publix/store_url_map.json"
	MissingStoreIDsCacheKey = "publix/missing_store_ids.json"
	LocationIDPrefix        = "publix_"
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

func loadCachedStoreSummaries(ctx context.Context, c cache.ListCache) ([]*StoreSummary, error) {
	keys, err := c.List(ctx, StoreCachePrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list cached store summaries: %w", err)
	}

	summaries := lop.Map(keys, func(key string, _ int) *StoreSummary {
		reader, err := c.Get(ctx, StoreCachePrefix+key)
		if err != nil {
			slog.WarnContext(ctx, "failed to read cached publix store summary", "key", key, "error", err)
			return nil
		}
		defer func() {
			_ = reader.Close()
		}()

		var summary StoreSummary
		if err := json.NewDecoder(reader).Decode(&summary); err != nil {
			slog.WarnContext(ctx, "failed to decode cached publix store summary", "key", key, "error", err)
			return nil
		}
		return &summary
	})

	summaries = lo.Compact(summaries)
	if len(summaries) == 0 {
		return nil, fmt.Errorf("failed to load publix locations")
	}

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
		Chain:   "publix",
	}
}
