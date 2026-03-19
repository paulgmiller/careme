package pointindex

import (
	"careme/internal/cache"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	locationtypes "careme/internal/locations/types"
)

type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type LocationLoader func(ctx context.Context, c cache.ListCache) ([]locationtypes.Location, error)

const cacheKey = "point_index"

func Save(ctx context.Context, c cache.Cache, index map[string]Point) error {
	if index == nil {
		index = map[string]Point{}
	}

	raw, err := json.Marshal(index)
	if err != nil {
		return fmt.Errorf("marshal point index: %w", err)
	}

	if err := c.Put(ctx, cacheKey, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write point index cache: %w", err)
	}
	return nil
}

func load(ctx context.Context, c cache.Cache) (map[string]Point, error) {
	reader, err := c.Get(ctx, cacheKey)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	index := make(map[string]Point)
	if err := json.NewDecoder(reader).Decode(&index); err != nil {
		return nil, fmt.Errorf("decode point index cache: %w", err)
	}
	return index, nil
}

func LoadOrBuild(ctx context.Context, c cache.ListCache, loadLocations LocationLoader) (map[string]Point, error) {
	index, err := load(ctx, c)
	if err == nil {
		return index, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return nil, fmt.Errorf("read point index cache: %w", err)
	}

	locations, err := loadLocations(ctx, c)
	if err != nil {
		return nil, err
	}

	index = buildFromLocations(locations)
	if err := Save(ctx, c, index); err != nil {
		return nil, err
	}
	return index, nil
}

func buildFromLocations(locations []locationtypes.Location) map[string]Point {
	index := make(map[string]Point, len(locations))
	for _, loc := range locations {
		if loc.ID == "" || loc.Lat == nil || loc.Lon == nil {
			continue
		}
		index[loc.ID] = Point{
			Lat: *loc.Lat,
			Lon: *loc.Lon,
		}
	}
	return index
}
