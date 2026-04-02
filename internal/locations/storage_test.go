package locations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	cachepkg "careme/internal/cache"
)

type namedBackend struct {
	id string
}

func (b namedBackend) GetLocationByID(context.Context, string) (*Location, error) {
	return nil, fmt.Errorf("not implemented")
}

func (b namedBackend) GetLocationsByZip(context.Context, string) ([]Location, error) {
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

	for i := 0; i < len(factories); i++ {
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

func TestInitializeLocationBackendsCancelsOtherFactoriesOnError(t *testing.T) {
	started := make(chan struct{})
	releaseErr := make(chan struct{})
	canceled := make(chan struct{}, 1)

	factories := []locationBackendFactory{
		func(context.Context) (locationBackend, error) {
			close(started)
			<-releaseErr
			return nil, errors.New("boom")
		},
		func(ctx context.Context) (locationBackend, error) {
			<-started
			<-ctx.Done()
			canceled <- struct{}{}
			return nil, ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() {
		_, err := initializeLocationBackends(context.Background(), factories)
		done <- err
	}()

	close(releaseErr)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("initializeLocationBackends error = nil, want error")
		}
		if got, want := err.Error(), "failed to initialize location backend 0: boom"; got != want {
			t.Fatalf("initializeLocationBackends error = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("initializeLocationBackends did not return after error")
	}

	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("expected sibling factory context to be canceled")
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
	locs, err := server.GetLocationsByZip(context.Background(), "not-a-zip")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if got, want := []string{locs[0].ID, locs[1].ID}, []string{"first", "second"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected order: got %v want %v", got, want)
	}
}

func TestGetLocationsByZipResolvesMissingRequestedZipCentroid(t *testing.T) {
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
	locs, err := server.GetLocationsByZip(context.Background(), "94012")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
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

func TestLocationStorageNearestZIPToCoordinates(t *testing.T) {
	centroids := LoadCentroids()

	zip, ok := centroids.NearestZIPToCoordinates(47.6097, -122.3331)
	if !ok {
		t.Fatal("expected nearest zip for valid coordinates")
	}
	if zip != "98101" {
		t.Fatalf("unexpected nearest zip: got %q want %q", zip, "98101")
	}
}

func TestGetLocationsByZipWhenAtLeastOneBackendSucceeds(t *testing.T) {
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
	if err == nil {
		t.Fatalf("expected an error: %v", err)
	}
	if len(locs) != 1 || locs[0].ID != "ok" {
		t.Fatalf("unexpected locations: %+v", locs)
	}
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
