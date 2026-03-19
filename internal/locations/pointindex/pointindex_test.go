package pointindex

import (
	"context"
	"testing"

	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	index := map[string]Point{
		"a": {Lat: 1.25, Lon: 2.5},
		"b": {Lat: 3.75, Lon: 4.125},
	}

	if err := Save(context.Background(), cacheStore, "test/points.json", index); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	got, err := Load(context.Background(), cacheStore, "test/points.json")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(got) != len(index) {
		t.Fatalf("unexpected index size: got %d want %d", len(got), len(index))
	}
	if got["a"] != index["a"] || got["b"] != index["b"] {
		t.Fatalf("unexpected loaded index: %+v", got)
	}
}

func TestLoadOrBuildBuildsAndPersistsIndex(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := cacheStore.Put(context.Background(), "stores/1", `{"ok":true}`, cache.Unconditional()); err != nil {
		t.Fatalf("seed store summary marker: %v", err)
	}

	loadCalls := 0
	index, err := LoadOrBuild(context.Background(), cacheStore, "points.json", "stores/", func(context.Context, cache.ListCache) ([]locationtypes.Location, error) {
		loadCalls++
		latA := 10.5
		lonA := -20.25
		latB := 30.75
		lonB := -40.125
		return []locationtypes.Location{
			{ID: "store_a", Lat: &latA, Lon: &lonA},
			{ID: "store_b", Lat: &latB, Lon: &lonB},
		}, nil
	})
	if err != nil {
		t.Fatalf("LoadOrBuild returned error: %v", err)
	}
	if loadCalls != 1 {
		t.Fatalf("unexpected load call count: got %d want 1", loadCalls)
	}
	if len(index) != 2 {
		t.Fatalf("unexpected index size: got %d want 2", len(index))
	}

	persisted, err := Load(context.Background(), cacheStore, "points.json")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(persisted) != 2 {
		t.Fatalf("unexpected persisted index size: got %d want 2", len(persisted))
	}
}

func TestBuildFromLocationsSkipsMissingCoordinates(t *testing.T) {
	t.Parallel()

	lat := 12.0
	lon := -13.0
	index := BuildFromLocations([]locationtypes.Location{
		{ID: "keep", Lat: &lat, Lon: &lon},
		{ID: "skip-lat"},
		{ID: ""},
	})

	if len(index) != 1 {
		t.Fatalf("unexpected index size: got %d want 1", len(index))
	}
	if index["keep"] != (Point{Lat: 12.0, Lon: -13.0}) {
		t.Fatalf("unexpected point: %+v", index["keep"])
	}
}
