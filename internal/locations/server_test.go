package locations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/auth"
	cachepkg "careme/internal/cache"
	"careme/internal/config"
	"careme/internal/templates"
)

type fakeProduceScoreLookup struct {
	scores map[string]*ProduceScore
}

func (f fakeProduceScoreLookup) ProduceScore(_ context.Context, loc Location) *ProduceScore {
	return f.scores[loc.ID]
}

type recordingProduceScoreLookup struct {
	mu     sync.Mutex
	calls  []string
	scores map[string]*ProduceScore
}

func (r *recordingProduceScoreLookup) ProduceScore(_ context.Context, loc Location) *ProduceScore {
	r.mu.Lock()
	r.calls = append(r.calls, loc.ID)
	r.mu.Unlock()

	return r.scores[loc.ID]
}

func (r *recordingProduceScoreLookup) callIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func TestRequestStoreWritesRequestBlob(t *testing.T) {
	mustInitLocationTemplates(t)

	fc := cachepkg.NewInMemoryCache()
	client := newFakeLocationClient()
	client.setDetailResponse("publix_123", Location{ID: "publix_123", Name: "Publix 123"})
	client.setHasInventory("publix_123", false)
	storage := newTestLocationServerWithBackendsAndCache([]locationBackend{client}, fc)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, fakeProduceScoreLookup{})

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
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, fakeProduceScoreLookup{})

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
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, fakeProduceScoreLookup{})

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
	if body := rr.Body.String(); !strings.Contains(body, "score 27") {
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
	if body := rr.Body.String(); strings.Contains(body, "score ") {
		t.Fatalf("expected no produce score badge, got %q", body)
	}
}

func TestLocationsPageSkipsNilProduceScores(t *testing.T) {
	mustInitLocationTemplates(t)

	client := newFakeLocationClient()
	client.setListResponse("10001", []Location{{
		ID:      "12345678",
		Name:    "Kroger Test",
		Address: "1 Market St",
		ZipCode: "10001",
	}})
	storage := newTestLocationServer(client)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, fakeProduceScoreLookup{})

	mux := http.NewServeMux()
	server.Register(mux, auth.DefaultMock())

	req := httptest.NewRequest(http.MethodGet, "/locations?zip=10001", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if body := rr.Body.String(); strings.Contains(body, "score ") {
		t.Fatalf("expected no produce score badge for nil score, got %q", body)
	}
}

func TestLocationsPageLimitsProviderLocationsBeforeScoring(t *testing.T) {
	mustInitLocationTemplates(t)

	client := newFakeLocationClient()
	locations := make([]Location, 0, 13)
	scores := make(map[string]*ProduceScore)
	for i := 0; i < 13; i++ {
		id := "store-" + strconv.Itoa(i)
		locations = append(locations, Location{
			ID:      id,
			Name:    "Store " + strconv.Itoa(i),
			Address: strconv.Itoa(i) + " Market St",
			ZipCode: "10001",
		})
		client.setHasInventory(id, i != 0)
		scores[id] = &ProduceScore{Score: i}
	}
	client.setListResponse("10001", locations)

	scoreLookup := &recordingProduceScoreLookup{scores: scores}
	storage := newTestLocationServer(client)
	server := NewServer(storage, LoadCentroids(), fakeUserLookup{}, scoreLookup)

	mux := http.NewServeMux()
	server.Register(mux, auth.DefaultMock())

	req := httptest.NewRequest(http.MethodGet, "/locations?zip=10001", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusOK, rr.Body.String())
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Store 9") {
		t.Fatalf("expected capped locations to render, got %q", body)
	}
	if strings.Contains(body, "Store 10") || strings.Contains(body, "Store 11") || strings.Contains(body, "Store 12") {
		t.Fatalf("expected locations from one provider to be capped at 10, got %q", body)
	}
	if strings.Contains(body, "score 10") || strings.Contains(body, "score 11") || strings.Contains(body, "score 12") {
		t.Fatalf("expected scores only for capped supported stores, got %q", body)
	}

	gotCalls := scoreLookup.callIDs()
	if len(gotCalls) != 9 {
		t.Fatalf("expected 9 score lookups, got %d: %v", len(gotCalls), gotCalls)
	}
	for _, id := range gotCalls {
		if id == "store-0" || id == "store-10" || id == "store-11" || id == "store-12" {
			t.Fatalf("unexpected score lookup for %s; calls=%v", id, gotCalls)
		}
	}
}

func mustInitLocationTemplates(t *testing.T) {
	t.Helper()
	if err := templates.Init(&config.Config{}, "dummyhash"); err != nil {
		t.Fatalf("failed to init templates: %v", err)
	}
}
