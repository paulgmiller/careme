package aldi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"careme/internal/cache"
	"careme/internal/locations/storeindex"

	locationtypes "careme/internal/locations/types"
)

const LocationIndexCacheKey = "aldi/store_locations.json"

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
	keys, err := c.List(ctx, StoreCachePrefix, "")
	if err != nil {
		return fmt.Errorf("list cached store summaries: %w", err)
	}

	entries := make([]storeindex.Entry, 0, len(keys))
	for _, key := range keys {
		reader, err := c.Get(ctx, StoreCachePrefix+key)
		if err != nil {
			return fmt.Errorf("read cached store summary: %w", err)
		}

		var summary StoreSummary
		decodeErr := json.NewDecoder(reader).Decode(&summary)
		_ = reader.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode cached store summary: %w", decodeErr)
		}
		if strings.TrimSpace(summary.InstoreShopID) == "" {
			continue
		}

		lat, lon := storeindex.Coordinates(summary.Lat, summary.Lon, summary.ZipCode, zipLookup)
		entries = append(entries, storeindex.Entry{
			ID:  summary.ID,
			Lat: lat,
			Lon: lon,
		})
	}

	if err := storeindex.Save(ctx, c, LocationIndexCacheKey, entries); err != nil {
		return err
	}
	slog.InfoContext(ctx, "rebuilt compact location index", "count", len(entries))
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
		return locationtypes.Location{}, fmt.Errorf("decode ALDI store summary: %w", err)
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
