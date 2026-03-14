package aldi

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
	if err := CacheStoreSummary(context.Background(), cacheStore, nearbySummary()); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	backend, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{
		"60610": {Lat: 41.9033, Lon: -87.6313},
	})
	if err != nil {
		t.Fatalf("newLocationBackend returned error: %v", err)
	}

	if !backend.IsID("aldi_F100") {
		t.Fatalf("expected ALDI id to be recognized")
	}

	loc, err := backend.GetLocationByID(context.Background(), "aldi_F100")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if loc.Name != "ALDI 201 W Division St" || loc.ZipCode != "60610" || loc.Chain != "aldi" {
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

	backend, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{
		"60610": {Lat: 41.9033, Lon: -87.6313},
	})
	if err != nil {
		t.Fatalf("newLocationBackend returned error: %v", err)
	}

	locs, err := backend.GetLocationsByZip(context.Background(), "60610")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 nearby location, got %d", len(locs))
	}
	if locs[0].ID != "aldi_F100" {
		t.Fatalf("unexpected location id: %q", locs[0].ID)
	}
	if locs[0].Chain != "aldi" {
		t.Fatalf("unexpected location chain: %q", locs[0].Chain)
	}
}

func TestNewLocationBackendErrorsWhenNoCachedSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()

	_, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{})
	if err == nil {
		t.Fatal("expected newLocationBackend to return an error")
	}
	if !strings.Contains(err.Error(), "failed to load aldi locations") {
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
	lat := 41.894989
	lon := -87.629197
	return &StoreSummary{
		ID:         "aldi_F100",
		StoreID:    5757831,
		Identifier: "F100",
		Name:       "ALDI 201 W Division St",
		Address:    "201 W Division St",
		City:       "Chicago",
		State:      "IL",
		ZipCode:    "60610",
		Lat:        &lat,
		Lon:        &lon,
	}
}

func farSummary() *StoreSummary {
	lat := 33.920675
	lon := -84.379135
	return &StoreSummary{
		ID:         "aldi_F216",
		StoreID:    5757832,
		Identifier: "F216",
		Name:       "ALDI 3333 Buford Hwy",
		Address:    "3333 Buford Hwy",
		City:       "Brookhaven",
		State:      "GA",
		ZipCode:    "30329",
		Lat:        &lat,
		Lon:        &lon,
	}
}
