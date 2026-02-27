package locations

import (
	cachepkg "careme/internal/cache"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestGetLocationByIDUsesCache(t *testing.T) {
	client := newFakeLocationClient()
	fc := cachepkg.NewInMemoryCache()
	client.setDetailResponse("12345", Location{
		ID:      "12345",
		Name:    "Friendly Market",
		Address: "123 Main St",
		ZipCode: "10001",
	})

	server := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)

	ctx := context.Background()
	got, err := server.GetLocationByID(ctx, "12345")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if got.Name != "Friendly Market" || got.Address != "123 Main St" {
		t.Fatalf("unexpected location returned: %+v", got)
	}
	if got.ZipCode != "10001" {
		t.Fatalf("unexpected zip code: %q", got.ZipCode)
	}
	requireEventuallyCached(t, fc, locationCachePrefix+"12345")
	// Remove backend value to prove the second read comes from persistent cache.
	delete(client.details, "12345")
	_, err = server.GetLocationByID(ctx, "12345")
	if err != nil {
		t.Fatalf("GetLocationByID second call returned error: %v", err)
	}
	requireEventuallyCached(t, fc, locationCachePrefix+"12345")
}

func TestGetLocationsByZipCachesLocations(t *testing.T) {
	client := newFakeLocationClient()
	fc := cachepkg.NewInMemoryCache()
	lat1 := 18.18060
	lon1 := -66.74990
	lat2 := 18.22000
	lon2 := -66.78000
	client.setListResponse("00601", []Location{
		{
			ID:      "111",
			Name:    "Store 111",
			Address: "1 North Ave",
			State:   "GA",
			ZipCode: "00601",
			Lat:     &lat1,
			Lon:     &lon1,
		},
		{
			ID:      "222",
			Name:    "Store 222",
			Address: "2 South St",
			State:   "GA",
			ZipCode: "00602",
			Lat:     &lat2,
			Lon:     &lon2,
		},
	})

	server := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)

	ctx := context.Background()
	locs, err := server.GetLocationsByZip(ctx, "00601")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(locs))
	}
	if locs[0].ID != "111" || locs[0].State != "GA" {
		t.Fatalf("unexpected first location: %+v", locs[0])
	}
	if locs[0].ZipCode != "00601" {
		t.Fatalf("unexpected first location zip code: %+v", locs[0])
	}
	if locs[1].ID != "222" || locs[1].Address != "2 South St" {
		t.Fatalf("unexpected second location: %+v", locs[1])
	}
	if locs[1].ZipCode != "00602" {
		t.Fatalf("unexpected second location zip code: %+v", locs[1])
	}

	requireEventuallyCached(t, fc, locationCachePrefix+"111")
	requireEventuallyCached(t, fc, locationCachePrefix+"222")
}

func TestGetLocationsByZipSortsByCentroidDistance(t *testing.T) {
	client := newFakeLocationClient()
	nearLat := 18.18060
	nearLon := -66.74990
	midLat := 18.30000
	midLon := -66.90000
	farLat := 47.60970
	farLon := -122.33310
	client.setListResponse("00601", []Location{
		{ID: "far", Name: "Far", ZipCode: "98004", Lat: &farLat, Lon: &farLon},
		{ID: "mid", Name: "Mid", ZipCode: "00602", Lat: &midLat, Lon: &midLon},
		{ID: "near", Name: "Near", ZipCode: "00601", Lat: &nearLat, Lon: &nearLon},
	})

	server := newTestLocationServer(client)
	locs, err := server.GetLocationsByZip(context.Background(), "00601")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("expected 2 locations after distance filter, got %d", len(locs))
	}
	if got, want := []string{locs[0].ID, locs[1].ID}, []string{"near", "mid"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected sorted order: got %v want %v", got, want)
	}
}

func TestGetLocationsByZipSortsUsingLocationZipCentroidFallback(t *testing.T) {
	client := newFakeLocationClient()
	farLat := 47.60970
	farLon := -122.33310
	client.setListResponse("00601", []Location{
		{ID: "far", Name: "Far", ZipCode: "98004", Lat: &farLat, Lon: &farLon},
		{ID: "near-by-zip", Name: "Near By Zip", ZipCode: "00601"},
		{ID: "unknown", Name: "Unknown", ZipCode: "zip-unknown"},
	})

	server := newTestLocationServer(client)
	locs, err := server.GetLocationsByZip(context.Background(), "00601")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location after filtering, got %d", len(locs))
	}
	if got, want := []string{locs[0].ID}, []string{"near-by-zip"}; got[0] != want[0] {
		t.Fatalf("unexpected sorted order: got %v want %v", got, want)
	}
}

func TestGetLocationsByZipLeavesOrderWhenQueryZipCentroidUnknown(t *testing.T) {
	client := newFakeLocationClient()
	client.setListResponse("not-a-zip", []Location{
		{ID: "first", Name: "First", ZipCode: "00602"},
		{ID: "second", Name: "Second", ZipCode: "00601"},
	})

	server := newTestLocationServer(client)
	_, err := server.GetLocationsByZip(context.Background(), "not-a-zip")
	if err == nil {
		t.Fatalf("GetLocationsByZip should have errored")
	}
}

func TestGetLocationByIDReturnsErrorWhenNoData(t *testing.T) {
	client := newFakeLocationClient()

	server := newTestLocationServer(client)

	_, err := server.GetLocationByID(context.Background(), "999")
	if err == nil {
		t.Fatalf("expected error when no location data returned")
	}
}

func TestGetLocationByIDLoadsFromPersistentCache(t *testing.T) {
	client := newFakeLocationClient()
	fc := cachepkg.NewInMemoryCache()
	cachedAt := mustParseTime(t, "2026-01-01T00:00:00Z")
	preloaded := Location{
		ID:       "12345",
		Name:     "Cached Store",
		Address:  "1 Cache Way",
		ZipCode:  "00601",
		CachedAt: cachedAt,
	}
	mustPutJSONInCache(t, fc, locationCachePrefix+"12345", preloaded)

	server := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)
	got, err := server.GetLocationByID(context.Background(), "12345")
	if err != nil {
		t.Fatalf("GetLocationByID returned error: %v", err)
	}
	if got.Name != "Cached Store" {
		t.Fatalf("expected cached location name, got %q", got.Name)
	}
}

func TestGetLocationsByZipStoresToPersistentCacheIfMissing(t *testing.T) {
	client := newFakeLocationClient()
	lat := 18.18060
	lon := -66.74990
	client.setListResponse("00601", []Location{
		{ID: "111", Name: "Store 111", ZipCode: "00601", Lat: &lat, Lon: &lon},
	})

	fc := cachepkg.NewInMemoryCache()
	server := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)
	locs, err := server.GetLocationsByZip(context.Background(), "00601")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}

	storedRaw := requireEventuallyCached(t, fc, locationCachePrefix+"111")
	var stored Location
	if err := json.Unmarshal([]byte(storedRaw), &stored); err != nil {
		t.Fatalf("failed to decode stored location: %v", err)
	}
	if stored.CachedAt.IsZero() {
		t.Fatalf("expected cached_at to be set when persisted")
	}
}

func TestGetLocationsByZipReturnsErrorWhenAllBackendsFail(t *testing.T) {
	failA := newFakeLocationClient()
	failA.err = fmt.Errorf("backend A down")
	failB := newFakeLocationClient()
	failB.err = fmt.Errorf("backend B down")

	server := newTestLocationServerWithBackends([]locationBackend{failA, failB})
	_, err := server.GetLocationsByZip(context.Background(), "00601")
	if err == nil {
		t.Fatalf("expected error when all backends fail")
	}
}

func TestGetLocationsByZipSucceedsWhenAtLeastOneBackendSucceeds(t *testing.T) {
	fail := newFakeLocationClient()
	fail.err = fmt.Errorf("backend down")

	success := newFakeLocationClient()
	lat := 18.18060
	lon := -66.74990
	success.setListResponse("00601", []Location{
		{ID: "ok", Name: "OK", ZipCode: "00601", Lat: &lat, Lon: &lon},
	})

	server := newTestLocationServerWithBackends([]locationBackend{fail, success})
	locs, err := server.GetLocationsByZip(context.Background(), "00601")
	if err != nil {
		t.Fatalf("did not expect error when one backend succeeds: %v", err)
	}
	if len(locs) != 1 || locs[0].ID != "ok" {
		t.Fatalf("unexpected locations: %+v", locs)
	}
}

type fakeLocationClient struct {
	details map[string]Location
	lists   map[string][]Location
	err     error
}

func newFakeLocationClient() *fakeLocationClient {
	return &fakeLocationClient{
		details: make(map[string]Location),
		lists:   make(map[string][]Location),
	}
}

func (f *fakeLocationClient) setDetailResponse(locationID string, location Location) {
	f.details[locationID] = location
}

func (f *fakeLocationClient) setListResponse(zip string, locations []Location) {
	f.lists[zip] = locations
}

func (f *fakeLocationClient) GetLocationsByZip(_ context.Context, zipcode string) ([]Location, error) {
	if f.err != nil {
		return nil, f.err
	}
	if locations, ok := f.lists[zipcode]; ok {
		return locations, nil
	}
	return nil, nil
}

func (f *fakeLocationClient) GetLocationByID(_ context.Context, locationID string) (*Location, error) {
	if f.err != nil {
		return nil, f.err
	}
	if location, ok := f.details[locationID]; ok {
		locationCopy := location
		return &locationCopy, nil
	}
	return nil, fmt.Errorf("no data found for location ID %s", locationID)
}

func (f *fakeLocationClient) IsID(locationID string) bool {
	return locationID != ""
}

func newTestLocationServer(client locationBackend) *locationStorage {
	return newTestLocationServerWithBackends([]locationBackend{client})
}

func newTestLocationServerWithBackends(backends []locationBackend) *locationStorage {
	return newTestLocationServerWithBackendsAndCache(backends, cachepkg.NewInMemoryCache())
}

func newTestLocationServerWithBackendsAndCache(backends []locationBackend, c cachepkg.Cache) *locationStorage {
	zipCentroids, err := loadEmbeddedZipCentroids()
	if err != nil {
		panic(err)
	}
	return &locationStorage{
		client:       backends,
		zipCentroids: zipCentroids,
		cache:        c,
	}
}

func mustPutJSONInCache(t *testing.T, c cachepkg.Cache, key string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("failed to marshal test cache value: %v", err)
	}
	if err := c.Put(context.Background(), key, string(raw), cachepkg.Unconditional()); err != nil {
		t.Fatalf("failed to preload cache key %q: %v", key, err)
	}
}

func requireEventuallyCached(t *testing.T, c cachepkg.Cache, key string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := c.Get(context.Background(), key)
		if err == nil {
			defer func() {
				_ = raw.Close()
			}()
			body, readErr := io.ReadAll(raw)
			if readErr != nil {
				t.Fatalf("failed to read cached value for key %q: %v", key, readErr)
			}
			return string(body)
		}
		if err != cachepkg.ErrNotFound {
			t.Fatalf("failed checking cache for key %q: %v", key, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected cache entry %q to be persisted within timeout", key)
	return ""
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("failed to parse time %q: %v", value, err)
	}
	return ts
}
