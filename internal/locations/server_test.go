package locations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/auth"
	cachepkg "careme/internal/cache"
	"careme/internal/config"
	"careme/internal/templates"
)

type fakeProduceScoreLookup struct {
	scores map[string]*ProduceScore
	err    error
}

func (f fakeProduceScoreLookup) ProduceScore(_ context.Context, loc *Location) (*ProduceScore, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.scores[loc.ID], nil
}

func TestRequestStoreWritesRequestBlob(t *testing.T) {
	mustInitLocationTemplates(t)

	fc := cachepkg.NewInMemoryCache()
	client := newFakeLocationClient()
	client.setDetailResponse("publix_123", Location{ID: "publix_123", Name: "Publix 123"})
	client.setHasInventory("publix_123", false)
	storage := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, nil)

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
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, nil)

	mux := http.NewServeMux()
	server.Register(mux, auth.DefaultMock())

	for i := range 2 {
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
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, nil)

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

func TestLocationsPageShowsCachedProduceScoreBadge(t *testing.T) {
	mustInitLocationTemplates(t)

	client := newFakeLocationClient()
	client.setListResponse("10001", []Location{{
		ID:      "12345678",
		Name:    "Kroger Test",
		Address: "1 Market St",
		ZipCode: "10001",
	}})
	storage := newTestLocationServer(client)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, fakeProduceScoreLookup{
		scores: map[string]*ProduceScore{
			"12345678": {
				Score: 27,
				Date:  time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC),
			},
		},
	})

	mux := http.NewServeMux()
	server.Register(mux, auth.DefaultMock())

	req := httptest.NewRequest(http.MethodGet, "/locations?zip=10001", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if body := rr.Body.String(); !strings.Contains(body, "Produce score 27") {
		t.Fatalf("expected produce score badge, got %q", body)
	}
}

func TestLocationsPageOmitsMissingProduceScoreBadge(t *testing.T) {
	mustInitLocationTemplates(t)

	client := newFakeLocationClient()
	client.setListResponse("10001", []Location{{
		ID:      "12345678",
		Name:    "Kroger Test",
		Address: "1 Market St",
		ZipCode: "10001",
	}})
	storage := newTestLocationServer(client)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, fakeProduceScoreLookup{
		scores: map[string]*ProduceScore{},
	})

	mux := http.NewServeMux()
	server.Register(mux, auth.DefaultMock())

	req := httptest.NewRequest(http.MethodGet, "/locations?zip=10001", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if body := rr.Body.String(); strings.Contains(body, "Produce score") {
		t.Fatalf("expected no produce score badge, got %q", body)
	}
}

func TestLocationsPageSkipsProduceScoreLookupErrors(t *testing.T) {
	mustInitLocationTemplates(t)

	client := newFakeLocationClient()
	client.setListResponse("10001", []Location{{
		ID:      "12345678",
		Name:    "Kroger Test",
		Address: "1 Market St",
		ZipCode: "10001",
	}})
	storage := newTestLocationServer(client)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, fakeProduceScoreLookup{
		err: errors.New("score lookup failed"),
	})

	mux := http.NewServeMux()
	server.Register(mux, auth.DefaultMock())

	req := httptest.NewRequest(http.MethodGet, "/locations?zip=10001", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if body := rr.Body.String(); strings.Contains(body, "Produce score") {
		t.Fatalf("expected no produce score badge after lookup error, got %q", body)
	}
}

func mustInitLocationTemplates(t *testing.T) {
	t.Helper()
	if err := templates.Init(&config.Config{}, "dummyhash"); err != nil {
		t.Fatalf("failed to init templates: %v", err)
	}
}
