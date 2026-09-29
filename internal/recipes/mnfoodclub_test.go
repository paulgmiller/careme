package recipes

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"careme/internal/config"
	"careme/internal/mnfoodclub"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultStaplesBackendsIncludesMNFoodClub(t *testing.T) {
	// HEB's constructor requires an endpoint, but this test never uses its browser.
	t.Setenv("BRIGHTDATA_BROWSER_WS_ENDPOINT", "wss://test:test@browser.example.invalid")
	backends, err := defaultStaplesBackends(&config.Config{})
	require.NoError(t, err)
	router := routingStaplesProvider{backends: backends}
	for _, id := range []string{"mnfoodclub_", "mnfoodclub_delivery", "mnfoodclub_other"} {
		provider, err := router.providerForLocation(id)
		require.NoError(t, err)
		assert.IsType(t, mnfoodclub.StaplesProvider{}, provider)
		assert.Equal(t, mnfoodclub.NewIdentityProvider().Signature(), provider.Signature())
	}
}

type mnfoodclubTransport func(*http.Request) (*http.Response, error)

func (f mnfoodclubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRoutingMNFoodClubStaples(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "page failure"
		}
		t.Run(name, func(t *testing.T) {
			var urls []string
			client := &http.Client{Transport: mnfoodclubTransport(func(req *http.Request) (*http.Response, error) {
				urls = append(urls, req.URL.String())
				if fail && len(urls) == 2 {
					return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<ul class="productGrid"><article class="card"><a data-product-id="1"></a><h4 class="card-title">Broccoli</h4><span data-product-price-without-tax>$2.99</span></article></ul>`))}, nil
			})}
			provider := dedupingStaplesProvider{provider: routingStaplesProvider{
				backends: []backendStaplesProvider{mnfoodclub.NewStaplesProvider(mnfoodclub.NewClient(client))},
			}}
			got, err := provider.FetchStaples(t.Context(), "mnfoodclub_delivery")
			if fail {
				require.ErrorContains(t, err, "HTTP 503")
				assert.Nil(t, got)
				assert.Len(t, urls, 2)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1) // Repeated product IDs are deduplicated by the standard wrapper.
			assert.Equal(t, "Broccoli", got[0].Description)
			assert.InDelta(t, 2.99, *got[0].PriceRegular, 0.001)
			assert.Equal(t, []string{
				"https://mnfood.club/shop-all/produce/?page=1&sort=bestselling",
				"https://mnfood.club/shop-all/produce/?page=2&sort=bestselling",
				"https://mnfood.club/shop-all/produce/?page=3&sort=bestselling",
				"https://mnfood.club/shop-all/meat/?page=1",
				"https://mnfood.club/shop-all/meat/?page=2",
				"https://mnfood.club/shop-all/meat/?page=3",
			}, urls)
			wines, err := provider.FetchWines(t.Context(), "mnfoodclub_delivery", []string{"Pinot Noir"})
			require.NoError(t, err)
			assert.Empty(t, wines)
			assert.Len(t, urls, 6)
		})
	}
}
