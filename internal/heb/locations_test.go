package heb

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
	if err := CacheStoreSummary(context.Background(), cacheStore, robstownSummary()); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}
	zipLookup := staticZIPLookup{
		"78380": {Lat: 27.8000, Lon: -97.6700},
	}
	if err := RebuildLocationIndex(context.Background(), cacheStore, zipLookup); err != nil {
		t.Fatalf("RebuildLocationIndex returned error: %v", err)
	}

	backend, err := newLocationBackend(context.Background(), cacheStore, zipLookup)
	if err != nil {
		t.Fatalf("newLocationBackend returned error: %v", err)
	}

	if !backend.IsID("heb_22") {
		t.Fatalf("expected heb id to be recognized")
	}

	loc, err := backend.GetLocationByID(context.Background(), "heb_22")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if loc.Name != "Robstown H-E-B" || loc.ZipCode != "78380" || loc.Chain != "heb" {
		t.Fatalf("unexpected location: %+v", loc)
	}
	reader, err := cacheStore.Get(context.Background(), LocationIndexCacheKey)
	if err != nil {
		t.Fatalf("expected compact location index to be cached: %v", err)
	}
	_ = reader.Close()
}

func TestLocationBackendGetLocationsByZipUsesDistance(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(context.Background(), cacheStore, robstownSummary()); err != nil {
		t.Fatalf("cache robstown summary: %v", err)
	}
	if err := CacheStoreSummary(context.Background(), cacheStore, farStoreSummary()); err != nil {
		t.Fatalf("cache far store summary: %v", err)
	}
	zipLookup := staticZIPLookup{
		"78380": {Lat: 27.8000, Lon: -97.6700},
	}
	if err := RebuildLocationIndex(context.Background(), cacheStore, zipLookup); err != nil {
		t.Fatalf("RebuildLocationIndex returned error: %v", err)
	}

	backend, err := newLocationBackend(context.Background(), cacheStore, zipLookup)
	if err != nil {
		t.Fatalf("newLocationBackend returned error: %v", err)
	}

	locs, err := backend.GetLocationsByZip(context.Background(), "78380")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 nearby location, got %d", len(locs))
	}
	if locs[0].ID != "heb_22" {
		t.Fatalf("unexpected location id: %q", locs[0].ID)
	}
}

func TestNewLocationBackendErrorsWhenNoCachedSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()

	_, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{})
	if err == nil {
		t.Fatal("expected newLocationBackend to return an error")
	}
	if !strings.Contains(err.Error(), "load heb locations index") {
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

func robstownSummary() *StoreSummary {
	lat := 27.7912
	lon := -97.6670
	return &StoreSummary{
		ID:      "heb_22",
		StoreID: "22",
		Name:    "Robstown H-E-B",
		Address: "308 E Main",
		City:    "Robstown",
		State:   "TX",
		ZipCode: "78380",
		Lat:     &lat,
		Lon:     &lon,
	}
}

func farStoreSummary() *StoreSummary {
	lat := 30.2672
	lon := -97.7431
	return &StoreSummary{
		ID:      "heb_216",
		StoreID: "216",
		Name:    "Hancock Center H-E-B",
		Address: "1000 E 41st St",
		City:    "Austin",
		State:   "TX",
		ZipCode: "78751",
		Lat:     &lat,
		Lon:     &lon,
	}
}
