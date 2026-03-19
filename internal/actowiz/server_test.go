package actowiz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestServerRegistersStoresJSON(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewServer(nil).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/actowiz/stores.json", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	if got := rr.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q, want %q", got, "application/json; charset=utf-8")
	}

	var got storesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(got.StoreIDs) < 30 {
		t.Fatalf("store id count = %d, want 30", len(got.StoreIDs))
	}
}

func TestServerAppendsRequestedStoresAndDedupes(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewServer(fakeRequestedStoreProvider{storeIDs: []string{"publix_123", "safeway_490"}}).Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/actowiz/stores.json", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var got storesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !slices.Contains(got.StoreIDs, "publix_123") {
		t.Fatalf("requested store %q missing from response: %v", "publix_123", got.StoreIDs)
	}

	duplicateCount := 0
	for _, storeID := range got.StoreIDs {
		if storeID == "safeway_490" {
			duplicateCount++
		}
	}
	if duplicateCount != 1 {
		t.Fatalf("store %q count = %d, want 1", "safeway_490", duplicateCount)
	}

	if got.StoreIDs[len(got.StoreIDs)-1] != "publix_123" {
		t.Fatalf("last store id = %q, want %q", got.StoreIDs[len(got.StoreIDs)-1], "publix_123")
	}
}

type fakeRequestedStoreProvider struct {
	storeIDs []string
	err      error
}

func (f fakeRequestedStoreProvider) RequestedStoreIDs(context.Context) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]string(nil), f.storeIDs...), nil
}
