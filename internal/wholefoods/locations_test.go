package wholefoods

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
	if err := CacheStoreSummary(context.Background(), cacheStore, westlakeSummary()); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	backend, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{
		"98101": {Lat: 47.6101, Lon: -122.3344},
	})
	if err != nil {
		t.Fatalf("newLocationBackend returned error: %v", err)
	}

	if !backend.IsID("wholefoods_10216") {
		t.Fatalf("expected wholefoods id to be recognized")
	}

	loc, err := backend.GetLocationByID(context.Background(), "wholefoods_10216")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if loc.Name != "Whole Foods Westlake" || loc.ZipCode != "98121" || loc.Chain != "wholefoods" {
		t.Fatalf("unexpected location: %+v", loc)
	}
}

func TestLocationBackendGetLocationsByZipUsesDistance(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(context.Background(), cacheStore, westlakeSummary()); err != nil {
		t.Fatalf("cache westlake summary: %v", err)
	}
	if err := CacheStoreSummary(context.Background(), cacheStore, farStoreSummary()); err != nil {
		t.Fatalf("cache far store summary: %v", err)
	}

	backend, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{
		"98101": {Lat: 47.6101, Lon: -122.3344},
	})
	if err != nil {
		t.Fatalf("newLocationBackend returned error: %v", err)
	}

	locs, err := backend.GetLocationsByZip(context.Background(), "98101")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 nearby location, got %d", len(locs))
	}
	if locs[0].ID != "wholefoods_10216" {
		t.Fatalf("unexpected location id: %q", locs[0].ID)
	}
	if locs[0].Chain != "wholefoods" {
		t.Fatalf("unexpected location chain: %q", locs[0].Chain)
	}
}

func TestLocationBackendReturnsAllWhenZipUnknown(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(context.Background(), cacheStore, westlakeSummary()); err != nil {
		t.Fatalf("cache westlake summary: %v", err)
	}
	if err := CacheStoreSummary(context.Background(), cacheStore, farStoreSummary()); err != nil {
		t.Fatalf("cache far store summary: %v", err)
	}

	backend, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{})
	if err != nil {
		t.Fatalf("newLocationBackend returned error: %v", err)
	}

	locs, err := backend.GetLocationsByZip(context.Background(), "unknown")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 0 {
		t.Fatalf("expected no locations when zip centroid is unknown, got %d", len(locs))
	}
}

func TestNewLocationBackendErrorsWhenNoCachedSummaries(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()

	_, err := newLocationBackend(context.Background(), cacheStore, staticZIPLookup{})
	if err == nil {
		t.Fatal("expected newLocationBackend to return an error")
	}
	if !strings.Contains(err.Error(), "failed to load wholefoods locations") {
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

func westlakeSummary() *StoreSummaryResponse {
	return &StoreSummaryResponse{
		StoreID:     10216,
		DisplayName: "Westlake",
		PrimaryLocation: StoreLocation{
			Address: StoreAddress{
				StreetAddressLine1: "2210 Westlake Ave",
				State:              "WA",
				ZipCode:            "98121",
			},
			Latitude:  47.618249,
			Longitude: -122.337898,
		},
	}
}

func farStoreSummary() *StoreSummaryResponse {
	return &StoreSummaryResponse{
		StoreID:     10153,
		DisplayName: "Portland",
		PrimaryLocation: StoreLocation{
			Address: StoreAddress{
				StreetAddressLine1: "1210 NW Couch St",
				State:              "OR",
				ZipCode:            "97209",
			},
			Latitude:  45.5231,
			Longitude: -122.6824,
		},
	}
}
