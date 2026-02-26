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

	server := newTestLocationServer(client)

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

	server.cacheLock.Lock()
	_, okFirst := server.locationCache["111"]
	_, okSecond := server.locationCache["222"]
	server.cacheLock.Unlock()
	if !okFirst || !okSecond {
		t.Fatalf("expected both locations cached, got cache=%v", server.locationCache)
	}
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

func (f *fakeLocationClient) IsID(locationID string) bool {
	return locationID != ""
}

func newTestLocationServer(client locationBackend) *locationStorage {
	zipCentroids, err := loadEmbeddedZipCentroids()
	if err != nil {
		panic(err)
	}
	return &locationStorage{
		locationCache: make(map[string]Location),
		client:        []locationBackend{client},
		zipCentroids:  zipCentroids,
	}
}
