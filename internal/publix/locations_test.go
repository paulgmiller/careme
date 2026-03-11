package publix

import (
	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	"context"
	"strings"
	"testing"
)

func TestNewLocationBackendBuildsIndexAndLookup(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(context.Background(), cacheStore, nearbySummary()); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	backend, err := NewLocationBackend(context.Background(), cacheStore, staticZIPLookup{
		"35401": {Lat: 33.2091, Lon: -87.5692},
	})
	if err != nil {
		t.Fatalf("NewLocationBackend returned error: %v", err)
	}

	if !backend.IsID("publix_1083") {
		t.Fatalf("expected publix id to be recognized")
	}

	loc, err := backend.GetLocationByID(context.Background(), "publix_1083")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if loc.Name != "Publix at University Town Center" || loc.ZipCode != "35401" || loc.Chain != "publix" {
		t.Fatalf("unexpected location: %+v", loc)
	}
}

func TestLocationBackendGetLocationsByZipUsesDistance(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(context.Background(), cacheStore, nearbySummary()); err != nil {
		t.Fatalf("cache nearby summary: %v", err)
	}
	if err := CacheStoreSummary(context.Background(), cacheStore, farSummary()); err != nil {
		t.Fatalf("cache far summary: %v", err)
	}

	backend, err := NewLocationBackend(context.Background(), cacheStore, staticZIPLookup{
		"35401": {Lat: 33.2091, Lon: -87.5692},
	})
	if err != nil {
		t.Fatalf("NewLocationBackend returned error: %v", err)
	}

	locs, err := backend.GetLocationsByZip(context.Background(), "35401")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 nearby location, got %d", len(locs))
	}
	if locs[0].ID != "publix_1083" {
		t.Fatalf("unexpected location id: %q", locs[0].ID)
	}
}

func TestNewLocationBackendErrorsWhenNoCachedSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()

	_, err := NewLocationBackend(context.Background(), cacheStore, staticZIPLookup{})
	if err == nil {
		t.Fatal("expected NewLocationBackend to return an error")
	}
	if !strings.Contains(err.Error(), "failed to load publix locations") {
		t.Fatalf("expected missing summaries error, got %v", err)
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
	lat := 33.212097
	lon := -87.553585
	return &StoreSummary{
		ID:      "publix_1083",
		StoreID: "1083",
		Name:    "Publix at University Town Center",
		Address: "1190 University Blvd",
		City:    "Tuscaloosa",
		State:   "AL",
		ZipCode: "35401",
		URL:     "https://www.publix.com/locations/1083-publix-at-university-town-center",
		Lat:     &lat,
		Lon:     &lon,
	}
}

func farSummary() *StoreSummary {
	lat := 28.5383
	lon := -81.3792
	return &StoreSummary{
		ID:      "publix_1200",
		StoreID: "1200",
		Name:    "Publix Downtown Orlando",
		Address: "400 E Central Blvd",
		City:    "Orlando",
		State:   "FL",
		ZipCode: "32801",
		URL:     "https://www.publix.com/locations/1200-downtown-orlando",
		Lat:     &lat,
		Lon:     &lon,
	}
}
