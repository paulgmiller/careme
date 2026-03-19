package locations

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"careme/internal/auth"
	cachepkg "careme/internal/cache"
	"careme/internal/config"
	"careme/internal/templates"
)

func TestRequestStoreWritesRequestBlob(t *testing.T) {
	mustInitLocationTemplates(t)

	fc := cachepkg.NewInMemoryCache()
	client := newFakeLocationClient()
	client.setDetailResponse("publix_123", Location{ID: "publix_123", Name: "Publix 123"})
	client.setHasInventory("publix_123", false)
	storage := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{})

	mux := http.NewServeMux()
	server.Register(mux, auth.DefaultMock())

	req := httptest.NewRequest(http.MethodPost, "/locations/request-store", strings.NewReader("store_id=publix_123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if rr.Header().Get("Location") != "" {
		t.Fatalf("expected no redirect location, got %q", rr.Header().Get("Location"))
	}
	if body := rr.Body.String(); !strings.Contains(body, "Request sent") {
		t.Fatalf("expected success fragment, got %q", body)
	}

	raw := requireEventuallyCached(t, fc, storeRequestPrefix+"publix_123")
	var payload locationRequest
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("failed to decode request blob: %v", err)
	}
	if payload.StoreID != "publix_123" {
		t.Fatalf("unexpected store id %q", payload.StoreID)
	}
	if payload.RequestedAt.IsZero() {
		t.Fatal("expected requested_at to be set")
	}
}

func TestRequestStoreIsIdempotent(t *testing.T) {
	mustInitLocationTemplates(t)

	fc := cachepkg.NewInMemoryCache()
	client := newFakeLocationClient()
	client.setDetailResponse("publix_123", Location{ID: "publix_123", Name: "Publix 123"})
	client.setHasInventory("publix_123", false)
	storage := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{})

	mux := http.NewServeMux()
	server.Register(mux, auth.DefaultMock())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/locations/request-store", strings.NewReader("store_id=publix_123"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d; body=%q", i+1, rr.Code, http.StatusOK, rr.Body.String())
		}
	}
}

func TestRequestStoreRejectsSupportedStore(t *testing.T) {
	mustInitLocationTemplates(t)

	fc := cachepkg.NewInMemoryCache()
	client := newFakeLocationClient()
	client.setDetailResponse("publix_123", Location{ID: "publix_123", Name: "Publix 123"})
	client.setHasInventory("publix_123", true)
	storage := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{})

	mux := http.NewServeMux()
	server.Register(mux, auth.DefaultMock())

	req := httptest.NewRequest(http.MethodPost, "/locations/request-store", strings.NewReader("store_id=publix_123"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func mustInitLocationTemplates(t *testing.T) {
	t.Helper()
	if err := templates.Init(&config.Config{}, "dummyhash"); err != nil {
		t.Fatalf("failed to init templates: %v", err)
	}
}
