package locations

import (
	"context"
	"fmt"
	"testing"
)

func TestGetLocationByIDUsesCache(t *testing.T) {
	client := newFakeLocationClient()
	client.setDetailResponse("12345", Location{
		ID:      "12345",
		Name:    "Friendly Market",
		Address: "123 Main St",
		ZipCode: "10001",
	})

	server := newTestLocationServer(client)

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

	_, err = server.GetLocationByID(ctx, "12345")
	if err != nil {
		t.Fatalf("GetLocationByID second call returned error: %v", err)
	}

	server.cacheLock.Lock()
	_, cached := server.locationCache["12345"]
	server.cacheLock.Unlock()
	if !cached {
		t.Fatalf("location 12345 not stored in cache")
	}
}

func TestGetLocationsByZipCachesLocations(t *testing.T) {
	client := newFakeLocationClient()
	client.setListResponse("30301", []Location{
		{
			ID:      "111",
			Name:    "Store 111",
			Address: "1 North Ave",
			State:   "GA",
			ZipCode: "30301",
		},
		{
			ID:      "222",
			Name:    "Store 222",
			Address: "2 South St",
			State:   "GA",
			ZipCode: "60601",
		},
	})

	server := newTestLocationServer(client)

	ctx := context.Background()
	locs, err := server.GetLocationsByZip(ctx, "30301")
	if err != nil {
		t.Fatalf("GetLocationsByZip returned error: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(locs))
	}
	if locs[0].ID != "111" || locs[0].State != "GA" {
		t.Fatalf("unexpected first location: %+v", locs[0])
	}
	if locs[0].ZipCode != "30301" {
		t.Fatalf("unexpected first location zip code: %+v", locs[0])
	}
	if locs[1].ID != "222" || locs[1].Address != "2 South St" {
		t.Fatalf("unexpected second location: %+v", locs[1])
	}
	if locs[1].ZipCode != "60601" {
		t.Fatalf("unexpected second location zip code: %+v", locs[1])
	}

	server.cacheLock.Lock()
	_, okFirst := server.locationCache["111"]
	_, okSecond := server.locationCache["222"]
	server.cacheLock.Unlock()
	if !okFirst || !okSecond {
		t.Fatalf("expected both locations cached, got cache=%v", server.locationCache)
	}
}

func TestGetLocationByIDReturnsErrorWhenNoData(t *testing.T) {
	client := newFakeLocationClient()

	server := newTestLocationServer(client)

	_, err := server.GetLocationByID(context.Background(), "999")
	if err == nil {
		t.Fatalf("expected error when no location data returned")
	}

	server.cacheLock.Lock()
	_, cached := server.locationCache["999"]
	server.cacheLock.Unlock()
	if cached {
		t.Fatalf("location 999 should not be cached on error")
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

func newTestLocationServer(client locationGetter) *locationStorage {
	return &locationStorage{
		locationCache: make(map[string]Location),
		client:        client,
	}
}
