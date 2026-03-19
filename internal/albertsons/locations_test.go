package albertsons

import (
	"careme/internal/cache"
	"careme/internal/locations/pointindex"
	"context"
	"strings"
	"testing"

	locationtypes "careme/internal/locations/types"
)

func TestNewLocationBackendBuildsIndexAndLookup(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(context.Background(), cacheStore, nearbySummary()); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	backend, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{
		"98006": {Lat: 47.5750, Lon: -122.1400},
	})
	if err != nil {
		t.Fatalf("NewLocationBackend returned error: %v", err)
	}

	if !backend.IsID("safeway_1444") {
		t.Fatalf("expected safeway id to be recognized")
	}
	if !backend.IsID("albertsons_611") {
		t.Fatalf("expected albertsons id to be recognized")
	}

	loc, err := backend.GetLocationByID(context.Background(), "safeway_1444")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if loc.Name != "Safeway 15100 SE 38th St" || loc.ZipCode != "98006" || loc.Chain != "albertsons" {
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
		"98006": {Lat: 47.5750, Lon: -122.1400},
	})
	if err != nil {
		t.Fatalf("NewLocationBackend returned error: %v", err)
	}

	locs, err := backend.GetLocationsByZip(context.Background(), "98006")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 nearby location, got %d", len(locs))
	}
	if locs[0].ID != "safeway_1444" {
		t.Fatalf("unexpected location id: %q", locs[0].ID)
	}
	if locs[0].Chain != "albertsons" {
		t.Fatalf("unexpected location chain: %q", locs[0].Chain)
	}
}

func TestLocationBackendGetLocationByIDUsesPointIndexWhenSummaryCoordinatesMissing(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	summary := nearbySummary()
	summary.Lat = nil
	summary.Lon = nil
	if err := CacheStoreSummary(context.Background(), cacheStore, summary); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}
	if err := pointindex.Save(context.Background(), cacheStore, map[string]pointindex.Point{
		"safeway_1444": {Lat: 47.5765527, Lon: -122.1381125},
	}); err != nil {
		t.Fatalf("SaveStorePointIndex returned error: %v", err)
	}

	backend, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{
		"98006": {Lat: 47.5750, Lon: -122.1400},
	})
	if err != nil {
		t.Fatalf("newLocationBackend returned error: %v", err)
	}

	loc, err := backend.GetLocationByID(context.Background(), "safeway_1444")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if loc.Lat == nil || loc.Lon == nil {
		t.Fatalf("expected coordinates from point index fallback, got %+v", loc)
	}
}

func TestNewLocationBackendErrorsWhenNoCachedSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()

	_, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{})
	if err == nil {
		t.Fatal("expected NewLocationBackend to return an error")
	}
	if !strings.Contains(err.Error(), "failed to load albertsons locations") {
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
	lat := 47.5765527
	lon := -122.1381125
	return &StoreSummary{
		ID:      "safeway_1444",
		Brand:   "safeway",
		Domain:  "local.safeway.com",
		StoreID: "1444",
		Name:    "Safeway 15100 SE 38th St",
		Address: "15100 SE 38th St",
		State:   "WA",
		ZipCode: "98006",
		Lat:     &lat,
		Lon:     &lon,
	}
}

func farSummary() *StoreSummary {
	lat := 33.4593747
	lon := -94.0419186
	return &StoreSummary{
		ID:      "albertsons_611",
		Brand:   "albertsons",
		Domain:  "local.albertsons.com",
		StoreID: "611",
		Name:    "Albertsons 3710 State Line Ave",
		Address: "3710 State Line Ave",
		State:   "AR",
		ZipCode: "71854",
		Lat:     &lat,
		Lon:     &lon,
	}
}
