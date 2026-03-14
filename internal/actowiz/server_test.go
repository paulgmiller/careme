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

	if len(got.StoreIDs) < 30 {
		t.Fatalf("store id count = %d, want 30", len(got.StoreIDs))
	}

}
