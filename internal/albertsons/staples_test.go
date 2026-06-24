package albertsons

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"careme/internal/albertsons/query"
)

func TestIdentityProviderSignature_UsesStapleCategories(t *testing.T) {
	t.Parallel()

	got := NewIdentityProvider().Signature()
	want, err := json.Marshal(struct {
		Categories []string        `json:"categories"`
		Rows       map[string]uint `json:"rows"`
	}{
		Categories: query.StapleCategories(),
		Rows:       stapleRows,
	})
	if err != nil {
		t.Fatalf("marshal staple categories: %v", err)
	}
	if got != string(want) {
		t.Fatalf("unexpected signature: got %q want %q", got, want)
	}
}

type stubSearchClient struct {
	results map[string][]query.PathwaySearchProduct
	mu      sync.Mutex
	calls   []string
	starts  []uint
}

func (s *stubSearchClient) SearchAll(_ context.Context, storeID, category string, opts query.SearchOptions) ([]query.PathwaySearchProduct, error) {
	s.mu.Lock()
	s.calls = append(s.calls, storeID+":"+category+":"+opts.Query)
	s.starts = append(s.starts, opts.Start)
	s.mu.Unlock()
	return s.results[category], nil
}

func (s *stubSearchClient) hasCall(want string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.calls, want)
}

func (s *stubSearchClient) startForCall(want string) (uint, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, call := range s.calls {
		if call == want {
			return s.starts[i], true
		}
	}
	return 0, false
}

func (s *stubSearchClient) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func TestStaplesProvider_MapsProductsToIngredients(t *testing.T) {
	t.Parallel()

	var requestedBaseURL string
	client := &stubSearchClient{
		results: map[string][]query.PathwaySearchProduct{
			query.Category_Vegatables: {
				{
					ID:             "veg-1",
					Name:           "Broccoli Crown",
					Price:          2.99,
					BasePrice:      3.49,
					ItemSizeQty:    "1",
					UnitOfMeasure:  "EA",
					DepartmentName: "Produce",
					ShelfName:      "Vegetables",
				},
			},
		},
	}

	provider := newStaplesProviderWithFactory(func(baseURL string) (searchClient, error) {
		requestedBaseURL = baseURL
		return client, nil
	})

	got, err := provider.FetchStaples(t.Context(), "safeway_1142")
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if requestedBaseURL != "https://www.safeway.com" {
		t.Fatalf("unexpected base URL: %q", requestedBaseURL)
	}
	if got, want := client.callCount(), len(query.StapleCategories()); got != want {
		t.Fatalf("expected %d category calls, got %d", want, got)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 mapped ingredient, got %d", len(got))
	}

	first := got[0]
	if first.ProductID != "veg-1" {
		t.Fatalf("unexpected product id: %+v", first.ProductID)
	}
	if first.Description != "Broccoli Crown" {
		t.Fatalf("unexpected description: %+v", first.Description)
	}
	if first.Size != "1 EA" {
		t.Fatalf("unexpected size: %+v", first.Size)
	}
	if first.PriceRegular == nil || *first.PriceRegular != float32(3.49) {
		t.Fatalf("unexpected regular price: %+v", first.PriceRegular)
	}
	if first.PriceSale == nil || *first.PriceSale != float32(2.99) {
		t.Fatalf("unexpected sale price: %+v", first.PriceSale)
	}
	if !slices.Equal(first.Categories, []string{"Produce", "Vegetables"}) {
		t.Fatalf("unexpected categories: %+v", first.Categories)
	}
}

func TestStaplesProvider_InvalidLocationID(t *testing.T) {
	t.Parallel()

	provider := newStaplesProviderWithFactory(func(baseURL string) (searchClient, error) {
		t.Fatalf("unexpected client creation for base URL %q", baseURL)
		return nil, nil
	})

	_, err := provider.FetchStaples(t.Context(), "1142")
	if err == nil {
		t.Fatal("expected invalid location error")
	}
	if got, want := err.Error(), `invalid albertsons location id "1142"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestStaplesProvider_FetchWines_UsesWineCategoryAndStyles(t *testing.T) {
	t.Parallel()

	client := &stubSearchClient{
		results: map[string][]query.PathwaySearchProduct{
			query.Category_Wine: {
				{ID: "wine-1", Name: "Pinot Noir", Price: 12.99},
			},
		},
	}

	provider := newStaplesProviderWithFactory(func(baseURL string) (searchClient, error) {
		if baseURL != "https://www.acmemarkets.com" {
			t.Fatalf("unexpected base URL: %q", baseURL)
		}
		return client, nil
	})

	got, err := provider.FetchWines(t.Context(), "acmemarkets_806", []string{"Pinot Noir", "Sauvignon Blanc"})
	if err != nil {
		t.Fatalf("FetchWines returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected one result per style, got %d", len(got))
	}
	for _, style := range []string{"Pinot Noir", "Sauvignon Blanc"} {
		searchCall := "806:" + query.Category_Wine + ":" + style
		if !client.hasCall(searchCall) {
			t.Fatalf("missing expected wine search call %q", searchCall)
		}
		if got, ok := client.startForCall(searchCall); !ok || got != 0 {
			t.Fatalf("unexpected search start for %q: got %d found %t", searchCall, got, ok)
		}
	}
}

func TestNewStaplesProvider_UsesInjectedHTTPClient(t *testing.T) {
	t.Parallel()

	var sawRequest bool
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			sawRequest = true
			if got, want := r.URL.Host, "www.acmemarkets.com"; got != want {
				return nil, fmt.Errorf("unexpected host: got %q want %q", got, want)
			}
			if got, want := r.URL.Query().Get("start"), "0"; got != want {
				return nil, fmt.Errorf("unexpected start query param: got %q want %q", got, want)
			}
			if got, want := r.Header.Get("Ocp-Apim-Subscription-Key"), "test-sub-key"; got != want {
				return nil, fmt.Errorf("unexpected subscription key: got %q want %q", got, want)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{
					"response":{"docs":[
						{"id":"wine-1","name":"Pinot Noir","price":10.99},
						{"id":"wine-2","name":"Rose","price":8.99}
					]}
				}`)),
			}, nil
		}),
	}

	provider := newStaplesProviderWithFactory(func(baseURL string) (searchClient, error) {
		querycfg := query.SearchClientConfig{
			SubscriptionKey: "test-sub-key",
			Reese84Provider: func(_ context.Context) (string, error) { return "test-reese84", nil },
			BaseURL:         baseURL,
			HTTPClient:      httpClient,
		}
		return query.NewSearchClient(querycfg)
	})

	got, err := provider.FetchWines(t.Context(), "acmemarkets_806", []string{"pinot"})
	if err != nil {
		t.Fatalf("FetchWines returned error: %v", err)
	}
	if !sawRequest {
		t.Fatal("expected injected HTTP client to be used")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 ingredients, got %d", len(got))
	}
	if got[0].Description != "Pinot Noir" {
		t.Fatalf("unexpected description: %+v", got[0].Description)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
