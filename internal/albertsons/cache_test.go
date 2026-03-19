package albertsons

import (
	"context"
	"testing"

	"careme/internal/cache"
	"careme/internal/locations/pointindex"
	locationtypes "careme/internal/locations/types"
)

func TestSaveStoreURLMapRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cacheStore := cache.NewInMemoryCache()
	urlMap := map[string]string{
		"https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html":  "albertsons_611",
		"https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html": "safeway_1444",
	}

	if err := SaveStoreURLMap(ctx, cacheStore, urlMap); err != nil {
		t.Fatalf("SaveStoreURLMapEntries returned error: %v", err)
	}

	got, err := LoadStoreURLMap(ctx, cacheStore)
	if err != nil {
		t.Fatalf("LoadStoreURLMap returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 url mappings, got %d", len(got))
	}
	if got["https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html"] != "albertsons_611" {
		t.Fatalf("unexpected albertsons mapping: %+v", got)
	}
	if got["https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html"] != "safeway_1444" {
		t.Fatalf("unexpected safeway mapping: %+v", got)
	}
}

func TestSaveStorePointIndexRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	cacheStore := cache.NewInMemoryCache()
	pointIndex := map[string]pointindex.Point{
		"albertsons_611": {Lat: 33.4593747, Lon: -94.0419186},
		"safeway_1444":   {Lat: 47.5765527, Lon: -122.1381125},
	}

	if err := pointindex.Save(ctx, cacheStore, pointIndex); err != nil {
		t.Fatalf("SaveStorePointIndex returned error: %v", err)
	}

	failingloader := func(ctx context.Context, c cache.ListCache, zipLookup pointindex.ZIPCentroidLookup) ([]locationtypes.Location, error) {
		t.Fatalf("location loader should not be called")
		return nil, nil
	}

	got, err := pointindex.LoadOrBuild(ctx, cacheStore, nil, failingloader)
	if err != nil {
		t.Fatalf("LoadStorePointIndex returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 point mappings, got %d", len(got))
	}
	if got["albertsons_611"] != pointIndex["albertsons_611"] {
		t.Fatalf("unexpected albertsons point: %+v", got["albertsons_611"])
	}
	if got["safeway_1444"] != pointIndex["safeway_1444"] {
		t.Fatalf("unexpected safeway point: %+v", got["safeway_1444"])
	}
}

func TestLoadOrBuildStorePointIndexBuildsFromCachedSummaries(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	cacheStore := cache.NewInMemoryCache()
	if err := CacheStoreSummary(ctx, cacheStore, nearbySummary()); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}
	if err := CacheStoreSummary(ctx, cacheStore, farSummary()); err != nil {
		t.Fatalf("CacheStoreSummary returned error: %v", err)
	}

	pointIndex, err := pointindex.LoadOrBuild(ctx, cacheStore, staticZIPLookup{}, LoadCachedStoreSummaries)
	if err != nil {
		t.Fatalf("LoadOrBuildStorePointIndex returned error: %v", err)
	}
	if len(pointIndex) != 2 {
		t.Fatalf("expected 2 points, got %d", len(pointIndex))
	}

	failingloader := func(ctx context.Context, c cache.ListCache, zipLookup pointindex.ZIPCentroidLookup) ([]locationtypes.Location, error) {
		t.Fatalf("location loader should not be called")
		return nil, nil
	}

	savedPointIndex, err := pointindex.LoadOrBuild(ctx, cacheStore, nil, failingloader)
	if err != nil {
		t.Fatalf("LoadStorePointIndex returned error: %v", err)
	}
	if len(savedPointIndex) != 2 {
		t.Fatalf("expected 2 saved points, got %d", len(savedPointIndex))
	}
}
