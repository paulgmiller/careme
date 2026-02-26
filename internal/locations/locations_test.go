package locations

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"careme/internal/kroger"
)

func TestGetLocationByIDUsesCache(t *testing.T) {
	client := newFakeKrogerClient()
	client.setDetailResponse("12345", http.StatusOK, `{"data":{"name":"Friendly Market","address":{"addressLine1":"123 Main St","zipCode":"10001"}}}`)

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
	client := newFakeKrogerClient()
	client.setListResponse("30301", http.StatusOK, `{"data":[{"locationId":"111","name":"Store 111","address":{"addressLine1":"1 North Ave","state":"GA","zipCode":"30301"}},{"locationId":"222","name":"Store 222","address":{"addressLine1":"2 South St","state":"GA","zipCode":"60601"}}]}`)

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
	client := newFakeKrogerClient()
	client.setDetailResponse("999", http.StatusOK, `{"data":null}`)

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

type fakeKrogerClient struct {
	details map[string]fakeHTTPPayload
	lists   map[string]fakeHTTPPayload
}

type fakeHTTPPayload struct {
	status int
	body   string
	err    error
}

func newFakeKrogerClient() *fakeKrogerClient {
	return &fakeKrogerClient{
		details: make(map[string]fakeHTTPPayload),
		lists:   make(map[string]fakeHTTPPayload),
	}
}

func (f *fakeKrogerClient) setDetailResponse(locationID string, status int, body string) {
	f.details[locationID] = fakeHTTPPayload{status: status, body: body}
}

func (f *fakeKrogerClient) setListResponse(zip string, status int, body string) {
	f.lists[zip] = fakeHTTPPayload{status: status, body: body}
}

func (f *fakeKrogerClient) LocationListWithResponse(ctx context.Context, params *kroger.LocationListParams, reqEditors ...kroger.RequestEditorFn) (*kroger.LocationListResponse, error) {
	zip := ""
	if params != nil && params.FilterZipCodeNear != nil {
		zip = *params.FilterZipCodeNear
	}

	payload, ok := f.lists[zip]

	if !ok {
		payload = fakeHTTPPayload{status: http.StatusOK, body: `{"data":[]}`}
	}
	if payload.err != nil {
		return nil, payload.err
	}

	resp := newJSONResponse(payload.status, payload.body)
	parsed, err := kroger.ParseLocationListResponse(resp)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func (f *fakeKrogerClient) LocationDetailsWithResponse(ctx context.Context, locationID string, reqEditors ...kroger.RequestEditorFn) (*kroger.LocationDetailsResponse, error) {
	payload, ok := f.details[locationID]

	if !ok {
		payload = fakeHTTPPayload{status: http.StatusOK, body: `{"data":null}`}
	}
	if payload.err != nil {
		return nil, payload.err
	}

	resp := newJSONResponse(payload.status, payload.body)
	parsed, err := kroger.ParseLocationDetailsResponse(resp)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

func newJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func newTestLocationServer(client krogerClient) *locationStorage {
	return &locationStorage{
		locationCache: make(map[string]Location),
		client:        client,
	}
}
