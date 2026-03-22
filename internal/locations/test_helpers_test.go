package locations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"careme/internal/auth"
	cachepkg "careme/internal/cache"

	utypes "careme/internal/users/types"
)

type fakeLocationClient struct {
	details map[string]Location
	lists   map[string][]Location
	inv     map[string]bool
	err     error
}

func newFakeLocationClient() *fakeLocationClient {
	return &fakeLocationClient{
		details: make(map[string]Location),
		lists:   make(map[string][]Location),
		inv:     make(map[string]bool),
	}
}

func (f *fakeLocationClient) setDetailResponse(locationID string, location Location) {
	f.details[locationID] = location
}

func (f *fakeLocationClient) setListResponse(zip string, locations []Location) {
	f.lists[zip] = locations
}

func (f *fakeLocationClient) setHasInventory(locationID string, hasInventory bool) {
	f.inv[locationID] = hasInventory
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

func (f *fakeLocationClient) HasInventory(locationID string) bool {
	if hasInventory, ok := f.inv[locationID]; ok {
		return hasInventory
	}
	return true
}

type inventoryBackend struct {
	supported map[string]bool
}

func (b inventoryBackend) GetLocationByID(context.Context, string) (*Location, error) {
	return nil, fmt.Errorf("not implemented")
}

func (b inventoryBackend) GetLocationsByZip(context.Context, string) ([]Location, error) {
	return nil, nil
}

func (b inventoryBackend) IsID(locationID string) bool {
	_, ok := b.supported[locationID]
	return ok
}

func (b inventoryBackend) HasInventory(locationID string) bool {
	return b.supported[locationID]
}

type failingListCache struct {
	putErr error
}

func (f failingListCache) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, cachepkg.ErrNotFound
}

func (f failingListCache) Exists(context.Context, string) (bool, error) {
	return false, nil
}

func (f failingListCache) Put(context.Context, string, string, cachepkg.PutOptions) error {
	return f.putErr
}

func (f failingListCache) PutReader(_ context.Context, _ string, _ io.Reader, _ cachepkg.PutOptions) error {
	return f.putErr
}

func (f failingListCache) List(context.Context, string, string) ([]string, error) {
	return nil, nil
}

func newTestLocationServer(client locationBackend) *locationStorage {
	return newTestLocationServerWithBackends([]locationBackend{client})
}

func newTestLocationServerWithBackends(backends []locationBackend) *locationStorage {
	return newTestLocationServerWithBackendsAndCache(backends, cachepkg.NewInMemoryCache())
}

func newTestLocationServerWithBackendsAndCache(backends []locationBackend, c cachepkg.ListCache) *locationStorage {
	zipCentroids := LoadCentroids()
	return &locationStorage{
		clients:      backends,
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

type fakeUserLookup struct{}

func (fakeUserLookup) FromRequest(context.Context, *http.Request, auth.AuthClient) (*utypes.User, error) {
	return nil, auth.ErrNoSession
}
