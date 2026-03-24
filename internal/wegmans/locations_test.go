package wegmans

import (
	"context"
	"strings"
	"testing"

	"careme/internal/cache"

	locationtypes "careme/internal/locations/types"
)

func TestNewLocationBackendBuildsIndexAndLookup(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(t.Context(), cacheStore, nearbySummary()); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}
	zipLookup := staticZIPLookup{
		"16506": {Lat: 42.0817, Lon: -80.1753},
	}
	if err := RebuildLocationIndex(t.Context(), cacheStore, zipLookup); err != nil {
		t.Fatalf("RebuildLocationIndex returned error: %v", err)
	}

	backend, err := newLocationBackend(t.Context(), cacheStore, zipLookup)
	if err != nil {
		t.Fatalf("NewLocationBackend returned error: %v", err)
	}

	if !backend.IsID("wegmans_69") {
		t.Fatalf("expected Wegmans id to be recognized")
	}

	loc, err := backend.GetLocationByID(t.Context(), "wegmans_69")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if loc.Name != "Wegmans Erie West" || loc.ZipCode != "16506" || loc.Chain != "wegmans" {
		t.Fatalf("unexpected location: %+v", loc)
	}
	reader, err := cacheStore.Get(t.Context(), LocationIndexCacheKey)
	if err != nil {
		t.Fatalf("expected compact location index to be cached: %v", err)
	}
	_ = reader.Close()
}

func TestLocationBackendGetLocationsByZipUsesDistance(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(t.Context(), cacheStore, nearbySummary()); err != nil {
		t.Fatalf("cache nearby summary: %v", err)
	}
	if err := CacheStoreSummary(t.Context(), cacheStore, farSummary()); err != nil {
		t.Fatalf("cache far summary: %v", err)
	}
	zipLookup := staticZIPLookup{
		"16506": {Lat: 42.0817, Lon: -80.1753},
	}
	if err := RebuildLocationIndex(t.Context(), cacheStore, zipLookup); err != nil {
		t.Fatalf("RebuildLocationIndex returned error: %v", err)
	}

	backend, err := newLocationBackend(t.Context(), cacheStore, zipLookup)
	if err != nil {
		t.Fatalf("NewLocationBackend returned error: %v", err)
	}

	locs, err := backend.GetLocationsByZip(context.Background(), "16506")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 nearby location, got %d", len(locs))
	}
	if locs[0].ID != "wegmans_69" {
		t.Fatalf("unexpected location id: %q", locs[0].ID)
	}
}

func TestNewLocationBackendErrorsWhenNoCachedSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()

	_, err := newLocationBackend(t.Context(), cacheStore, staticZIPLookup{})
	if err == nil {
		t.Fatal("expected NewLocationBackend to return an error")
	}
	if !strings.Contains(err.Error(), "load wegmans locations index") {
		t.Fatalf("expected missing index error, got %v", err)
	}
}

type staticZIPLookup map[string]coords

type coords struct {
	Lat float64
	Lon float64
}

func (s staticZIPLookup) ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool) {
	coord, ok := s[zip]
	if !ok {
		return locationtypes.ZipCentroid{}, false
	}
	return locationtypes.ZipCentroid{Lat: coord.Lat, Lon: coord.Lon}, true
}

func nearbySummary() *StoreSummary {
	lat := 42.06996
	lon := -80.1919
	return &StoreSummary{
		ID:          "wegmans_69",
		StoreNumber: 69,
		Name:        "Wegmans Erie West",
		Address:     "5028 West Ridge Road",
		City:        "Erie",
		State:       "PA",
		ZipCode:     "16506",
		Lat:         &lat,
		Lon:         &lon,
	}
}

func farSummary() *StoreSummary {
	lat := 40.7177
	lon := -73.8458
	return &StoreSummary{
		ID:          "wegmans_101",
		StoreNumber: 101,
		Name:        "Wegmans Forest Hills",
		Address:     "109-14 Horace Harding Expy",
		City:        "Forest Hills",
		State:       "NY",
		ZipCode:     "11375",
		Lat:         &lat,
		Lon:         &lon,
	}
}
