package storeindex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"careme/internal/cache"
	"careme/internal/parallelism"

	locationtypes "careme/internal/locations/types"
)

// TODO should we just embed zip centroid?
type Entry struct {
	ID  string   `json:"id"`
	Lat *float64 `json:"lat,omitempty"`
	Lon *float64 `json:"lon,omitempty"`
}

type ZipCentroidLookup interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

func (e Entry) ToLocation() locationtypes.Location {
	return locationtypes.Location{
		ID:  e.ID,
		Lat: e.Lat,
		Lon: e.Lon,
	}
}

func Coordinates(lat, lon *float64, zipCode string, zipLookup ZipCentroidLookup) (*float64, *float64) {
	if lat != nil && lon != nil {
		return lat, lon
	}

	centroid, ok := zipLookup.ZipCentroidByZIP(zipCode)
	if !ok {
		// panic("missing zip centroid for zip code: " + zipCode)
		return nil, nil // boo what will location.go do with this?
	}

	return &centroid.Lat, &centroid.Lon
}

func HydrateLocations(ctx context.Context, candidates []locationtypes.Location, hydrate func(context.Context, string) (locationtypes.Location, error)) ([]locationtypes.Location, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	out, err := parallelism.MapWithErrors(candidates, func(candidate locationtypes.Location) (locationtypes.Location, error) {
		return hydrate(ctx, candidate.ID)
	})

	if len(out) == 0 {
		return nil, fmt.Errorf("zero hydrated locations: %w", err)
	}
	return out, nil
}

func Save(ctx context.Context, c cache.Cache, key string, entries []Entry) error {
	raw, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal location index: %w", err)
	}
	if err := c.PutReader(ctx, key, bytes.NewReader(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write location index: %w", err)
	}
	return nil
}

func Load(ctx context.Context, c cache.Cache, key string) ([]Entry, error) {
	reader, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var entries []Entry
	if err := json.NewDecoder(reader).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("zero entry index")
	}
	return entries, nil
}

func RebuildFromStoreSummaries[T any](ctx context.Context, c cache.ListCache, storePrefix, indexKey string, toEntry func(T) Entry) ([]Entry, error) {
	keys, err := c.List(ctx, storePrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list cached store summaries: %w", err)
	}

	// could parallize but less important now that its in scrapers
	entries := make([]Entry, 0, len(keys))
	for _, key := range keys {
		reader, err := c.Get(ctx, storePrefix+key)
		if err != nil {
			return nil, fmt.Errorf("read cached store summary: %w", err)
		}
		defer func() {
			_ = reader.Close()
		}()

		var summary T
		decodeErr := json.NewDecoder(reader).Decode(&summary)
		if decodeErr != nil {
			return nil, fmt.Errorf("decode cached store summary: %w", decodeErr)
		}

		entries = append(entries, toEntry(summary))
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("zero location index")
	}

	if err := Save(ctx, c, indexKey, entries); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "rebuilt compact location index", "count", len(entries))
	return entries, nil
}
