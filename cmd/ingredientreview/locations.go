package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/providers/kroger"
)

// cachedLocations reads the same keys as the location providers without
// initializing clients or fetching missing locations.
type cachedLocations struct{ cache cache.Cache }

func (c cachedLocations) GetLocationByID(ctx context.Context, id string) (*locations.Location, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return nil, cache.ErrNotFound
	}
	prefix := "location/"
	var krogerBackend kroger.LocationBackend
	if krogerBackend.IsID(id) {
		version, _ := krogerBackend.CachePolicy()
		prefix += version + "/"
	}
	reader, err := c.cache.Get(ctx, prefix+id)
	if err != nil {
		return nil, fmt.Errorf("read cached location %q: %w", id, err)
	}
	defer func() { _ = reader.Close() }()
	var location locations.Location
	if err := json.NewDecoder(reader).Decode(&location); err != nil {
		return nil, fmt.Errorf("decode cached location %q: %w", id, err)
	}
	return &location, nil
}
