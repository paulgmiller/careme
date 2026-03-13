package actowiz

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerRegistersStoresJSON(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	NewServer().Register(mux)

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

	want := storesResponse{
		StoreIDs:           []string{"store-1", "store-2", "store-3"},
		ScrapeIntervalDays: 7,
	}
	if len(got.StoreIDs) != len(want.StoreIDs) {
		t.Fatalf("store id count = %d, want %d", len(got.StoreIDs), len(want.StoreIDs))
	}
	for i := range want.StoreIDs {
		if got.StoreIDs[i] != want.StoreIDs[i] {
			t.Fatalf("store_ids[%d] = %q, want %q", i, got.StoreIDs[i], want.StoreIDs[i])
		}
	}
	if got.ScrapeIntervalDays != want.ScrapeIntervalDays {
		t.Fatalf("scrape interval days = %d, want %d", got.ScrapeIntervalDays, want.ScrapeIntervalDays)
	}
}
