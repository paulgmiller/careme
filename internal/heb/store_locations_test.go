package heb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeStoreLocationsPage(t *testing.T) {
	t.Parallel()

	page, err := DecodeStoreLocationsPage([]byte(storeLocationsFixture()))
	require.NoError(t, err)
	require.Len(t, page.Summaries, 1)
	assert.Equal(t, 10, page.TotalStoresCount)
	assert.Equal(t, 1, page.CurrentPage)

	summary := page.Summaries[0]
	assert.Equal(t, "heb_699", summary.ID)
	assert.Equal(t, "699", summary.StoreID)
	assert.Equal(t, "Nogalitos H-E-B", summary.Name)
	assert.Equal(t, "1601 NOGALITOS", summary.Address)
	assert.Equal(t, "SAN ANTONIO", summary.City)
	assert.Equal(t, "TX", summary.State)
	assert.Equal(t, "78204", summary.ZipCode)
	require.NotNil(t, summary.Lat)
	require.NotNil(t, summary.Lon)
	assert.Equal(t, 29.3978, *summary.Lat)
	assert.Equal(t, -98.51499, *summary.Lon)
}

func TestDecodeStoreLocationsPageReportsHTMLSnippet(t *testing.T) {
	t.Parallel()

	_, err := DecodeStoreLocationsPage([]byte(`<html><body>blocked</body></html>`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not JSON")
	assert.Contains(t, err.Error(), "blocked")
}

func TestStoreLocationsClientBuildsNextDataRequest(t *testing.T) {
	t.Parallel()

	var captured *http.Request
	httpClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			captured = req
			return responseWithBody(http.StatusOK, storeLocationsFixture()), nil
		}),
	}
	client := NewStoreLocationsClient(StoreLocationsClientConfig{
		BaseURL:    "https://www.heb.test",
		HTTPClient: httpClient,
		Reese84Provider: func(context.Context) (string, error) {
			return "test-reese", nil
		},
		UserAgent: "test-agent",
	})

	body, err := client.StoreLocationsPage(context.Background(), "test-build", "78204", 2)
	require.NoError(t, err)
	require.NotEmpty(t, body)
	require.NotNil(t, captured)
	assert.Equal(t, "/_next/data/test-build/en/store-locations.json", captured.URL.Path)
	assert.Equal(t, "78204", captured.URL.Query().Get("address"))
	assert.Equal(t, "2", captured.URL.Query().Get("page"))
	assert.Equal(t, "https://www.heb.test/store-locations", captured.Header.Get("Referer"))
	assert.Equal(t, "test-agent", captured.Header.Get("User-Agent"))
	assert.Equal(t, "1", captured.Header.Get("X-Nextjs-Data"))
	assert.Equal(t, "reese84=test-reese", captured.Header.Get("Cookie"))
}

func TestStoreLocationsPageCacheRoundTrip(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	require.NoError(t, CacheStoreLocationsPage(context.Background(), cacheStore, "San Antonio, TX", 1, []byte(storeLocationsFixture())))

	page, err := LoadCachedStoreLocationsPage(context.Background(), cacheStore, "San Antonio, TX", 1)
	require.NoError(t, err)
	require.Len(t, page.Summaries, 1)
	assert.Equal(t, "heb_699", page.Summaries[0].ID)
}

func TestNextDataBuildIDCacheRoundTrip(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	require.NoError(t, SaveNextDataBuildID(context.Background(), cacheStore, "fresh-build"))

	got, err := LoadNextDataBuildID(context.Background(), cacheStore)
	require.NoError(t, err)
	assert.Equal(t, "fresh-build", got)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func responseWithBody(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func storeLocationsFixture() string {
	return `{
		"pageProps": {
			"currentPageStores": [
				{
					"store": {
						"longitude": -98.51499,
						"latitude": 29.3978,
						"storeNumber": 699,
						"name": "Nogalitos H-E-B",
						"address": {
							"streetAddress": "1601 NOGALITOS",
							"locality": "SAN ANTONIO",
							"region": "TX",
							"postalCode": "78204-2427"
						}
					}
				}
			],
			"totalStoresCount": 10,
			"currentPage": 1,
			"searchError": false
		}
	}`
}
