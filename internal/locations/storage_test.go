package locations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	cachepkg "careme/internal/cache"
	"careme/internal/locations/geo"
	"careme/internal/mnfoodclub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type namedBackend struct {
	id string
}

func (b namedBackend) GetLocationByID(context.Context, string) (*Location, error) {
	return nil, fmt.Errorf("not implemented")
}

func (b namedBackend) GetLocationsByCoordinates(context.Context, geo.Coordinate) ([]Location, error) {
	return nil, nil
}

func (b namedBackend) IsID(string) bool {
	return false
}

func (b namedBackend) HasInventory(string) bool {
	return false
}

func TestInitializeLocationBackendsRunsFactoriesInParallelAndCollectsBackends(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})

	factories := []locationBackendFactory{
		func(context.Context) (locationBackend, error) {
			started <- "first"
			<-release
			return namedBackend{id: "first"}, nil
		},
		func(context.Context) (locationBackend, error) {
			started <- "second"
			<-release
			return namedBackend{id: "second"}, nil
		},
	}

	type result struct {
		backends []locationBackend
		err      error
	}
	done := make(chan result, 1)
	go func() {
		backends, err := initializeLocationBackends(context.Background(), factories)
		done <- result{backends: backends, err: err}
	}()

	for range factories {
		select {
		case <-started:
		case <-time.After(200 * time.Millisecond):
			t.Fatal("expected all backend factories to start before any finished")
		}
	}

	close(release)

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("initializeLocationBackends returned error: %v", result.err)
		}
		if len(result.backends) != 2 {
			t.Fatalf("expected 2 backends, got %d", len(result.backends))
		}

		gotIDs := make(map[string]bool, len(result.backends))
		for _, backend := range result.backends {
			named, ok := backend.(namedBackend)
			if !ok {
				t.Fatalf("expected backend type namedBackend, got %T", backend)
			}
			gotIDs[named.id] = true
		}
		if !gotIDs["first"] || !gotIDs["second"] {
			t.Fatalf("expected both backends to be returned, got %v", gotIDs)
		}
	case <-time.After(time.Second):
		t.Fatal("initializeLocationBackends did not finish")
	}
}

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
	if got.Lat == nil || got.Lon == nil {
		t.Fatalf("expected coordinates to be backfilled: %+v", got)
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

func TestGetLocationsByCoordinatesCachesLocations(t *testing.T) {
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
	locs, err := server.GetLocationsByCoordinates(ctx, coordinatesForZIP(t, "00601"))
	if err != nil {
		t.Fatalf("GetLocationsByCoordinates returned error: %v", err)
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

func TestGetLocationsByCoordinatesSortsByDistance(t *testing.T) {
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
	locs, err := server.GetLocationsByCoordinates(context.Background(), coordinatesForZIP(t, "00601"))
	if err != nil {
		t.Fatalf("GetLocationsByCoordinates returned error: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("expected 2 locations after distance filter, got %d", len(locs))
	}
	if got, want := []string{locs[0].ID, locs[1].ID}, []string{"near", "mid"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected sorted order: got %v want %v", got, want)
	}
}

func TestGetLocationsByCoordinatesSortsUsingLocationZipCentroidFallback(t *testing.T) {
	client := newFakeLocationClient()
	farLat := 47.60970
	farLon := -122.33310
	client.setListResponse("00601", []Location{
		{ID: "far", Name: "Far", ZipCode: "98004", Lat: &farLat, Lon: &farLon},
		{ID: "near-by-zip", Name: "Near By Zip", ZipCode: "00601"},
		{ID: "unknown", Name: "Unknown", ZipCode: "zip-unknown"},
	})

	server := newTestLocationServer(client)
	locs, err := server.GetLocationsByCoordinates(context.Background(), coordinatesForZIP(t, "00601"))
	if err != nil {
		t.Fatalf("GetLocationsByCoordinates returned error: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location after filtering, got %d", len(locs))
	}
	if got, want := []string{locs[0].ID}, []string{"near-by-zip"}; got[0] != want[0] {
		t.Fatalf("unexpected sorted order: got %v want %v", got, want)
	}
	if locs[0].Lat == nil || locs[0].Lon == nil {
		t.Fatalf("expected ZIP centroid coordinates to be included: %+v", locs[0])
	}
}

func TestGetLocationsByCoordinatesUsesFormerlyMissingZIPCentroid(t *testing.T) {
	client := newFakeLocationClient()
	nearLat := 37.331714
	nearLon := -122.341466
	midLat := 37.388239
	midLon := -122.075351
	farLat := 47.60970
	farLon := -122.33310
	client.setListResponse("94012", []Location{
		{ID: "far", Name: "Far", ZipCode: "98004", Lat: &farLat, Lon: &farLon},
		{ID: "mid", Name: "Mid", ZipCode: "94041", Lat: &midLat, Lon: &midLon},
		{ID: "near", Name: "Near", ZipCode: "94074", Lat: &nearLat, Lon: &nearLon},
	})

	server := newTestLocationServer(client)
	locs, err := server.GetLocationsByCoordinates(context.Background(), coordinatesForZIP(t, "94012"))
	if err != nil {
		t.Fatalf("GetLocationsByCoordinates returned error: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("expected 2 nearby locations after centroid backfill, got %d", len(locs))
	}
	if got, want := []string{locs[0].ID, locs[1].ID}, []string{"near", "mid"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected order: got %v want %v", got, want)
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

func TestGetLocationsByCoordinatesStoresToPersistentCacheIfMissing(t *testing.T) {
	client := newFakeLocationClient()
	lat := 18.18060
	lon := -66.74990
	client.setListResponse("00601", []Location{
		{ID: "111", Name: "Store 111", ZipCode: "00601", Lat: &lat, Lon: &lon},
	})

	fc := cachepkg.NewInMemoryCache()
	server := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)
	locs, err := server.GetLocationsByCoordinates(context.Background(), coordinatesForZIP(t, "00601"))
	if err != nil {
		t.Fatalf("GetLocationsByCoordinates returned error: %v", err)
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

func TestGetLocationsByCoordinatesReturnsErrorWhenAllBackendsFail(t *testing.T) {
	failA := newFakeLocationClient()
	failA.err = fmt.Errorf("backend A down")
	failB := newFakeLocationClient()
	failB.err = fmt.Errorf("backend B down")

	server := newTestLocationServerWithBackends([]locationBackend{failA, failB})
	_, err := server.GetLocationsByCoordinates(context.Background(), coordinatesForZIP(t, "00601"))
	if err == nil {
		t.Fatalf("expected error when all backends fail")
	}
}

func TestGetLocationsByCoordinatesIgnoresErrorsWhenAtLeastOneBackendSucceeds(t *testing.T) {
	fail := newFakeLocationClient()
	fail.err = fmt.Errorf("backend down")

	success := newFakeLocationClient()
	lat := 18.18060
	lon := -66.74990
	success.setListResponse("00601", []Location{
		{ID: "ok", Name: "OK", ZipCode: "00601", Lat: &lat, Lon: &lon},
	})

	server := newTestLocationServerWithBackends([]locationBackend{fail, success})
	locs, err := server.GetLocationsByCoordinates(context.Background(), coordinatesForZIP(t, "00601"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(locs) != 1 || locs[0].ID != "ok" {
		t.Fatalf("unexpected locations: %+v", locs)
	}
}

func coordinatesForZIP(t *testing.T, zip string) geo.Coordinate {
	t.Helper()
	coordinates, ok := LoadCentroids().ZipCentroidByZIP(zip)
	if !ok {
		t.Fatalf("coordinates not found for ZIP %q", zip)
	}
	return coordinates
}

func TestHasInventory(t *testing.T) {
	server := newTestLocationServerWithBackends([]locationBackend{
		inventoryBackend{
			supported: map[string]bool{
				"70500874":       true,
				"wholefoods_123": true,
			},
		},
		inventoryBackend{
			supported: map[string]bool{
				"walmart_123": true,
			},
		},
	})

	tests := []struct {
		name         string
		storeID      string
		hasInventory bool
	}{
		{name: "kroger", storeID: "70500874", hasInventory: true},
		{name: "wholefoods", storeID: "wholefoods_123", hasInventory: true},
		{name: "walmart", storeID: "walmart_123", hasInventory: true},
		{name: "unsupported", storeID: "publix_123", hasInventory: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := server.HasInventory(tt.storeID); got != tt.hasInventory {
				t.Fatalf("HasInventory(%q) = %v, want %v", tt.storeID, got, tt.hasInventory)
			}
		})
	}
}

func TestRequestStoreReturnsWriteErrors(t *testing.T) {
	storage := &locationStorage{
		cache: failingListCache{putErr: errors.New("boom")},
	}

	err := storage.RequestStore(context.Background(), "publix_123")
	if err == nil {
		t.Fatal("RequestStore error = nil, want error")
	}
}

func TestRequestedStoreIDsListsStoredRequests(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	storage := newTestLocationServerWithBackendsAndCache([]locationBackend{newFakeLocationClient()}, fc)

	mustPutJSONInCache(t, fc, storeRequestPrefix+"publix_123", locationRequest{StoreID: "publix_123"})
	mustPutJSONInCache(t, fc, storeRequestPrefix+"walmart_456", locationRequest{StoreID: "walmart_456"})

	got, err := storage.RequestedStoreIDs(context.Background())
	if err != nil {
		t.Fatalf("RequestedStoreIDs returned error: %v", err)
	}

	if got, want := strings.Join(got, ","), "publix_123,walmart_456"; got != want {
		t.Fatalf("RequestedStoreIDs = %q, want %q", got, want)
	}
}

func TestGetLocationsByCoordinatesCancellation(t *testing.T) {
	for _, tt := range []struct {
		name       string
		canceled   bool
		backendErr error
		wantLevel  string
	}{
		{"request canceled", true, &url.Error{Op: "Post", URL: "https://example.test/token", Err: fmt.Errorf("token request: %w", context.Canceled)}, "DEBUG"},
		{"backend canceled independently", false, context.Canceled, "ERROR"},
		{"backend timeout", false, context.DeadlineExceeded, "ERROR"},
		{"backend failure during cancellation", true, errors.New("backend unavailable"), "ERROR"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(previous) })
			failed := newFakeLocationClient()
			failed.err = tt.backendErr
			success := newFakeLocationClient()
			success.setListResponse("00601", []Location{{ID: "ok", ZipCode: "00601"}})
			storage := newTestLocationServerWithBackends([]locationBackend{failed, success})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.canceled {
				cancel()
			}
			got, err := storage.GetLocationsByCoordinates(ctx, coordinatesForZIP(t, "00601"))
			if tt.canceled {
				require.ErrorIs(t, err, context.Canceled)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Len(t, got, 1)
			}
			found := false
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var record map[string]any
				require.NoError(t, json.Unmarshal([]byte(line), &record))
				if record["msg"] == "error fetching locations from backend" {
					found = true
					assert.Equal(t, tt.wantLevel, record["level"])
				}
			}
			assert.True(t, found, "backend failure should remain observable")
		})
	}
}

func TestMNFoodClubLocationsByCoordinatesAreNotCached(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	server := newTestLocationServerWithBackendsAndCache([]locationBackend{mnfoodclub.NewLocationBackend()}, fc)
	for _, coordinates := range []geo.Coordinate{
		{Lat: 44.98, Lon: -93.27},
		{Lat: 44.01, Lon: -92.48},
	} {
		locations, err := server.GetLocationsByCoordinates(t.Context(), coordinates)
		require.NoError(t, err)
		require.Len(t, locations, 1)
		assert.Equal(t, "mnfoodclub_delivery", locations[0].ID)
		assert.Equal(t, coordinates, locations[0].Coordinate())
	}
	assert.Never(t, func() bool {
		keys, err := fc.List(t.Context(), locationCachePrefix, "")
		require.NoError(t, err)
		return len(keys) > 0
	}, 50*time.Millisecond, time.Millisecond)
}

func TestMNFoodClubLocationByIDIgnoresCache(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	backend := mnfoodclub.NewLocationBackend()
	server := newTestLocationServerWithBackendsAndCache([]locationBackend{backend}, fc)
	for _, id := range []string{"mnfoodclub_delivery", "mnfoodclub_44.98_-93.27"} {
		t.Run(id, func(t *testing.T) {
			mustPutJSONInCache(t, fc, locationCachePrefix+id, Location{
				ID: id, Name: "Stale search point", Lat: new(44.98), Lon: new(-93.27),
			})
			got, err := server.GetLocationByID(t.Context(), id)
			if id == "mnfoodclub_delivery" {
				require.NoError(t, err)
				want, err := backend.GetLocationByID(t.Context(), id)
				require.NoError(t, err)
				assert.Equal(t, want, got)
			} else {
				require.ErrorContains(t, err, "invalid MNFoodClub location ID")
				assert.Nil(t, got)
			}
		})
	}
}

func TestMNFoodClubLocationByIDDoesNotWriteCache(t *testing.T) {
	fc := cachepkg.NewInMemoryCache()
	server := newTestLocationServerWithBackendsAndCache([]locationBackend{mnfoodclub.NewLocationBackend()}, fc)
	got, err := server.GetLocationByID(t.Context(), "mnfoodclub_delivery")
	require.NoError(t, err)
	assert.Equal(t, "mnfoodclub_delivery", got.ID)
	assert.Never(t, func() bool {
		exists, err := fc.Exists(t.Context(), locationCachePrefix+got.ID)
		require.NoError(t, err)
		return exists
	}, 50*time.Millisecond, time.Millisecond)
}

type cachePolicyBackend struct {
	*fakeLocationClient
	cacheable bool
}

func (b cachePolicyBackend) IsCacheable() bool { return b.cacheable }

func TestLocationBackendCachePolicy(t *testing.T) {
	for _, test := range []struct {
		name          string
		policy        *bool
		wantCacheable bool
	}{
		{"default", nil, true},
		{"enabled", new(true), true},
		{"disabled", new(false), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fc := cachepkg.NewInMemoryCache()
			client := newFakeLocationClient()
			coordinates := coordinatesForZIP(t, "00601")
			fresh := Location{ID: "fresh", Name: "Fresh backend location", Lat: &coordinates.Lat, Lon: &coordinates.Lon}
			client.setDetailResponse("cached", Location{ID: "cached", Name: "Backend location", Lat: &coordinates.Lat, Lon: &coordinates.Lon})
			client.setDetailResponse("fresh", fresh)
			client.setListResponse("00601", []Location{{ID: "search", Name: "Search result", Lat: &coordinates.Lat, Lon: &coordinates.Lon}})
			var backend locationBackend = client
			if test.policy != nil {
				backend = cachePolicyBackend{client, *test.policy}
			}
			server := newTestLocationServerWithBackendsAndCache([]locationBackend{backend}, fc)
			mustPutJSONInCache(t, fc, locationCachePrefix+"cached", Location{ID: "cached", Name: "Cached location", Lat: &coordinates.Lat, Lon: &coordinates.Lon})

			got, err := server.GetLocationByID(t.Context(), "cached")
			require.NoError(t, err)
			if test.wantCacheable {
				assert.Equal(t, "Cached location", got.Name)
			} else {
				assert.Equal(t, "Backend location", got.Name)
			}

			got, err = server.GetLocationByID(t.Context(), "fresh")
			require.NoError(t, err)
			assert.Equal(t, fresh, *got)
			locations, err := server.GetLocationsByCoordinates(t.Context(), coordinates)
			require.NoError(t, err)
			require.Len(t, locations, 1)
			assert.Equal(t, "search", locations[0].ID)

			for _, id := range []string{"fresh", "search"} {
				if test.wantCacheable {
					requireEventuallyCached(t, fc, locationCachePrefix+id)
				} else {
					assert.Never(t, func() bool {
						exists, err := fc.Exists(t.Context(), locationCachePrefix+id)
						require.NoError(t, err)
						return exists
					}, 50*time.Millisecond, time.Millisecond)
				}
			}
		})
	}
}
