package fullcircle_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/providers/fullcircle"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func floatPointer(v float32) *float32 { return &v }

func TestCatalogRouting(t *testing.T) {
	paths := map[string]string{"/product/7/farm-product-list": "dairy", "/product/11/farm-product-list": "meat", "/product/20/farm-product-list": "grains"}
	var requested []string
	httpClient := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "www.fullcircle.com", req.URL.Host)
		assert.Equal(t, "application/json", req.Header.Get("Accept"))
		assert.Equal(t, "XMLHttpRequest", req.Header.Get("X-Requested-With"))
		fixture, ok := paths[req.URL.Path]
		require.True(t, ok, req.URL.String())
		requested = append(requested, fixture)
		data, err := os.ReadFile("testdata/" + fixture + ".json")
		require.NoError(t, err)
		return response(http.StatusOK, string(data)), nil
	})}
	backend := fullcircle.NewStaplesProvider(fullcircle.NewClient(httpClient))
	provider := recipes.NewStaplesProvider([]recipes.StaplesBackend{backend})
	items, err := provider.FetchStaples(t.Context(), "fullcircle_delivery")
	require.NoError(t, err)
	require.Len(t, items, 6)
	assert.Equal(t, []string{"dairy", "meat", "grains"}, requested)
	assert.Equal(t, ai.InputIngredient{ProductID: "fullcircle_14798", Description: "Purple Moon Cheddar Cheese", Brand: "Fiscalini Cheese Company", Size: "6oz", PriceRegular: floatPointer(7.29), Categories: []string{"Dairy"}}, items[0])
	assert.Equal(t, "Organic Canned Chicken Breast", items[2].Description)
	assert.Equal(t, "fullcircle_2574", items[4].ProductID)
	assert.Equal(t, []string{"Grains & Rice"}, items[4].Categories)
	var rendered bytes.Buffer
	require.NoError(t, ai.InputIngredientsToTSV(items, &rendered))
	assert.Contains(t, rendered.String(), "Purple Moon Cheddar Cheese")
	assert.Contains(t, rendered.String(), "White Basmati Rice")
	assert.NotContains(t, rendered.String(), "Traditional CSA")
}

const productJSON = `[{"ProductListingId":123,"ProductName":" Milk &amp; Cream ","VendorName":" Farmer&#39;s Dairy ","ProductSubTitle":" 16oz ","Price":4.5,"RegularPrice":6,"SoldOut":null},{"ProductListingId":456,"ProductName":"Unavailable","SoldOut":true}]`

func TestPricesSoldOutAndDuplicateCategories(t *testing.T) {
	provider := fullcircle.NewStaplesProvider(fullcircle.NewClient(&http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { return response(200, productJSON), nil })}))
	items, err := provider.FetchStaples(t.Context(), "fullcircle_delivery")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, ai.InputIngredient{ProductID: "fullcircle_123", Description: "Milk & Cream", Brand: "Farmer's Dairy", Size: "16oz", PriceRegular: floatPointer(6), PriceSale: floatPointer(4.5), Categories: []string{"Dairy", "Meat", "Grains & Rice"}}, items[0])
}

func TestCatalogFailuresReturnNoPartialInventory(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		status, attempts int
	}{
		{"not found", "", "HTTP 404", 404, 1},
		{"server error", "", "HTTP 503", 503, 3},
		{"HTML instead of JSON", "<html>Log in</html>", "decode Full Circle Meat", 200, 1},
		{"wrong shape", `{"products":[]}`, "decode Full Circle Meat", 200, 1},
		{"null catalog", "null", "empty Full Circle Meat catalog", 200, 1},
		{"empty catalog", "[]", "empty Full Circle Meat catalog", 200, 1},
		{"missing ID", `[{"ProductName":"Rice","Price":3}]`, "ID, name", 200, 1},
		{"missing name", `[{"ProductListingId":1,"Price":3}]`, "ID, name", 200, 1},
		{"missing price", `[{"ProductListingId":1,"ProductName":"Rice"}]`, "nonnegative price", 200, 1},
		{"negative price", `[{"ProductListingId":1,"ProductName":"Rice","Price":-1}]`, "nonnegative price", 200, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			provider := fullcircle.NewStaplesProvider(fullcircle.NewClient(&http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/product/7/farm-product-list" {
					return response(200, productJSON), nil
				}
				require.Equal(t, "/product/11/farm-product-list", req.URL.Path, "must stop after failure")
				attempts++
				return response(tt.status, tt.body), nil
			})}))
			items, err := provider.FetchStaples(t.Context(), "fullcircle_delivery")
			require.ErrorContains(t, err, tt.want)
			assert.Nil(t, items)
			assert.Equal(t, tt.attempts, attempts)
		})
	}
}

func TestRetryRecoversAndInvalidIDsDoNotFetch(t *testing.T) {
	attempts := 0
	provider := fullcircle.NewStaplesProvider(fullcircle.NewClient(&http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(503, ""), nil
		}
		return response(200, productJSON), nil
	})}))
	for _, id := range []string{"", "fullcircle_123", "smithbrothersfarms_delivery"} {
		assert.False(t, provider.IsID(id))
		_, err := provider.FetchStaples(t.Context(), id)
		require.ErrorContains(t, err, "invalid Full Circle location ID")
	}
	assert.Zero(t, attempts)
	items, err := provider.FetchStaples(t.Context(), "fullcircle_delivery")
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, 4, attempts)
	_, err = provider.FetchWines(t.Context(), "fullcircle_delivery", []string{"red"})
	require.ErrorContains(t, err, "not supported")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	items, err = provider.FetchStaples(ctx, "fullcircle_delivery")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, items)
	assert.Equal(t, 4, attempts)
}

func TestWeeklyCacheIdentity(t *testing.T) {
	provider := fullcircle.NewIdentityProvider()
	monday := time.Date(2026, 11, 2, 9, 0, 0, 0, time.FixedZone("Pacific", -8*60*60))
	hash := func(date time.Time) []byte { return provider.HashForStore("fullcircle_delivery", date).Sum(nil) }
	for day := range 7 {
		assert.Equal(t, hash(monday), hash(monday.AddDate(0, 0, day)))
	}
	assert.NotEqual(t, hash(monday), hash(monday.AddDate(0, 0, 7)))
	assert.NotEqual(t, hash(monday), hash(monday.Add(-time.Minute)))
	assert.NotEqual(t, hash(monday), provider.HashForStore("smithbrothersfarms_delivery", monday).Sum(nil))
}

func TestLiveCatalog(t *testing.T) {
	if os.Getenv("FULLCIRCLE_LIVE_TEST") != "1" {
		t.Skip("set FULLCIRCLE_LIVE_TEST=1 to fetch the public catalog")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	provider := fullcircle.NewStaplesProvider(fullcircle.NewClient(http.DefaultClient))
	items, err := provider.FetchStaples(ctx, "fullcircle_delivery")
	require.NoError(t, err)
	require.NotEmpty(t, items)
	counts := map[string]int{}
	for _, item := range items {
		assert.NotEmpty(t, item.ProductID)
		assert.NotEmpty(t, item.Description)
		require.NotNil(t, item.PriceRegular)
		for _, category := range item.Categories {
			counts[category]++
		}
	}
	for _, category := range []string{"Dairy", "Meat", "Grains & Rice"} {
		assert.Positive(t, counts[category], category)
	}
	t.Logf("fetched %d products: %v", len(items), counts)
}
