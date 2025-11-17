package locations

import (
	"careme/internal/kroger"
	"context"
	"sync"
	"testing"
)

// mockKrogerClient is a mock implementation of krogerClient for testing
type mockKrogerClient struct{}

func (m *mockKrogerClient) LocationListWithResponse(ctx context.Context, params *kroger.LocationListParams, reqEditors ...kroger.RequestEditorFn) (*kroger.LocationListResponse, error) {
	return nil, nil
}

func (m *mockKrogerClient) LocationDetailsWithResponse(ctx context.Context, locationId string, reqEditors ...kroger.RequestEditorFn) (*kroger.LocationDetailsResponse, error) {
	return nil, nil
}

func TestLocationAdapter(t *testing.T) {
	// Create a location server with mock client
	server := &locationServer{
		locationCache: make(map[string]Location),
		client:        &mockKrogerClient{},
		cacheLock:     sync.Mutex{},
	}

	// Manually add a location to the cache (with proper locking)
	testLocation := Location{
		ID:      "70100023",
		Name:    "Test Kroger Store",
		Address: "123 Main St",
		State:   "CA",
	}
	server.cacheLock.Lock()
	server.locationCache[testLocation.ID] = testLocation
	server.cacheLock.Unlock()

	// Create adapter
	adapter := NewLocationAdapter(server)

	// Test GetLocationNameByID with cached location
	ctx := context.Background()
	name, err := adapter.GetLocationNameByID(ctx, "70100023")
	if err != nil {
		t.Fatalf("GetLocationNameByID failed: %v", err)
	}
	if name != "Test Kroger Store" {
		t.Errorf("Expected name to be 'Test Kroger Store', got '%s'", name)
	}
}
