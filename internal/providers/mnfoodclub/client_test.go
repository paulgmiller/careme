package mnfoodclub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"careme/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func testClient(t *testing.T, transport roundTripFunc) *Client {
	t.Helper()
	client, err := NewClient(config.MNFoodClubConfig{APIPath: "https://api.bigcommerce.com/stores/test/v3/", AccessToken: "test-token"}, &http.Client{Transport: transport})
	require.NoError(t, err)
	return client
}

func jsonPage(t *testing.T, data any, page, pages int) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]any{"data": data, "meta": map[string]any{"pagination": map[string]int{"current_page": page, "total_pages": pages}}})
	require.NoError(t, err)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}
}

func TestFetchPaginatesCatalogAndMetadata(t *testing.T) {
	for _, category := range []int{AllCategories, 178} {
		t.Run(fmt.Sprint(category), func(t *testing.T) {
			var calls []string
			var bodies []*trackedBody
			client := testClient(t, func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, http.MethodGet, req.Method)
				assert.Equal(t, t.Context(), req.Context())
				assert.Equal(t, "test-token", req.Header.Get("X-Auth-Token"))
				assert.Equal(t, "application/json", req.Header.Get("Accept"))
				assert.Equal(t, "250", req.URL.Query().Get("limit"))
				page, err := strconv.Atoi(req.URL.Query().Get("page"))
				require.NoError(t, err)
				calls = append(calls, req.URL.Path+"?page="+strconv.Itoa(page))
				var resp *http.Response
				switch req.URL.Path {
				case "/stores/test/v3/catalog/products":
					assert.Equal(t, "true", req.URL.Query().Get("is_visible"))
					assert.Equal(t, "variants", req.URL.Query().Get("include"))
					assert.Equal(t, "id", req.URL.Query().Get("sort"))
					assert.Equal(t, "asc", req.URL.Query().Get("direction"))
					if category == AllCategories {
						assert.Empty(t, req.URL.Query().Get("categories:in"))
					} else {
						assert.Equal(t, "178", req.URL.Query().Get("categories:in"))
					}
					resp = jsonPage(t, []catalogProduct{{ID: page, Name: " Organic Beef - avg 1lb ", BrandID: 2, Categories: []int{1, 2}, Price: 10, IsVisible: true, Availability: "available", Variants: []catalogVariant{{CalculatedPrice: 8}}}}, page, 2)
				case "/stores/test/v3/catalog/categories":
					resp = jsonPage(t, []catalogCategory{{ID: page, Name: []string{"Shop All", "Meat"}[page-1], ParentID: page - 1}}, page, 2)
				case "/stores/test/v3/catalog/brands":
					resp = jsonPage(t, []catalogBrand{{ID: page, Name: "Local Farm"}}, page, 2)
				default:
					t.Fatalf("unexpected request: %s", req.URL.Path)
				}
				body := &trackedBody{Reader: resp.Body}
				bodies = append(bodies, body)
				resp.Body = body
				return resp, nil
			})
			got, err := client.Fetch(t.Context(), category)
			require.NoError(t, err)
			require.Len(t, got, 2)
			assert.Equal(t, "2", got[1].ProductID)
			assert.Equal(t, "Organic Beef - avg 1lb", got[0].Description)
			assert.Equal(t, "avg 1lb", got[0].Size)
			assert.Equal(t, "Local Farm", got[0].Brand)
			assert.Equal(t, []string{"Shop All", "Meat"}, got[0].Categories)
			assert.Equal(t, "Meat", got[0].AisleNumber)
			assert.Equal(t, float32(10), *got[0].PriceRegular)
			assert.Equal(t, float32(8), *got[0].PriceSale)
			assert.Nil(t, got[0].Grade)
			assert.Equal(t, []string{
				"/stores/test/v3/catalog/products?page=1", "/stores/test/v3/catalog/products?page=2",
				"/stores/test/v3/catalog/categories?page=1", "/stores/test/v3/catalog/categories?page=2",
				"/stores/test/v3/catalog/brands?page=1", "/stores/test/v3/catalog/brands?page=2",
			}, calls)
			for _, body := range bodies {
				assert.True(t, body.closed)
			}
		})
	}
}

func TestCatalogConfiguration(t *testing.T) {
	client, err := NewClient(config.MNFoodClubConfig{APIPath: "https://api.bigcommerce.com/stores/test/v3/", AccessToken: "token"}, &http.Client{})
	require.NoError(t, err)
	got, err := client.catalogURL()
	require.NoError(t, err)
	assert.Equal(t, "https://api.bigcommerce.com/stores/test/v3/catalog/", got.String())
	for _, cfg := range []config.MNFoodClubConfig{
		{APIPath: "https://api.bigcommerce.com/stores/test/", AccessToken: "token"},
		{APIPath: "https://api.bigcommerce.com/stores/test/v3", AccessToken: "token"},
		{APIPath: "https://api.bigcommerce.com/stores/test/v3//", AccessToken: "token"},
		{APIPath: "https://api.bigcommerce.com/stores//v3/", AccessToken: "token"},
		{APIPath: "https://api.bigcommerce.com/stores/test/v2/", AccessToken: "token"},
		{APIPath: "://invalid", AccessToken: "token"},
		{APIPath: "file:///stores/test", AccessToken: "token"},
		{APIPath: "https://api.bigcommerce.com/", AccessToken: "token"},
		{APIPath: "https://user:password@api.bigcommerce.com/stores/test/v3/", AccessToken: "token"},
		{APIPath: "https://api.bigcommerce.com/stores/test/v3/?query=1", AccessToken: "token"},
	} {
		client, err := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid configuration must not make requests")
			return nil, nil
		})})
		require.NoError(t, err)
		got, err := client.Fetch(t.Context(), AllCategories)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.NotContains(t, err.Error(), "password")
	}
}

func TestNewClientRequiresCredentials(t *testing.T) {
	for _, cfg := range []config.MNFoodClubConfig{
		{},
		{APIPath: "https://api.bigcommerce.com/stores/test/v3"},
		{AccessToken: "token"},
		{APIPath: " \t ", AccessToken: "token"},
		{APIPath: "https://api.bigcommerce.com/stores/test/v3", AccessToken: " \n "},
	} {
		client, err := NewClient(cfg, &http.Client{})
		require.ErrorContains(t, err, "MNFoodClub requires MNFOODCLUB_API_PATH and MNFOODCLUB_ACCESS_TOKEN")
		assert.Nil(t, client)
	}
}

func TestFetchFailuresReturnNoPartialCatalog(t *testing.T) {
	failure := errors.New("read failed")
	for _, test := range []struct {
		name   string
		status int
		body   io.Reader
		err    error
		want   string
	}{
		{"transport", 0, nil, failure, "read failed"},
		{"unauthorized", 401, strings.NewReader("private error body"), nil, "HTTP 401"},
		{"forbidden", 403, strings.NewReader("private error body"), nil, "HTTP 403"},
		{"server", 503, strings.NewReader("private error body"), nil, "HTTP 503"},
		{"json", 200, strings.NewReader("not JSON"), nil, "decode catalog response"},
		{"body", 200, errorReader{failure}, nil, "read failed"},
		{"pagination", 200, strings.NewReader(`{"data":[]}`), nil, "invalid pagination"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			var body *trackedBody
			client := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return jsonPage(t, []catalogProduct{{ID: 1}}, 1, 2), nil
				}
				if test.err != nil {
					return nil, test.err
				}
				body = &trackedBody{Reader: test.body}
				return &http.Response{StatusCode: test.status, Body: body}, nil
			})
			got, err := client.Fetch(t.Context(), AllCategories)
			require.ErrorContains(t, err, test.want)
			assert.Contains(t, err.Error(), "products page 2")
			assert.NotContains(t, err.Error(), "private error body")
			assert.NotContains(t, err.Error(), "test-token")
			assert.Nil(t, got)
			assert.Equal(t, 2, calls)
			if body != nil {
				assert.True(t, body.closed)
			}
		})
	}
	for _, resource := range []string{"categories", "brands"} {
		t.Run(resource, func(t *testing.T) {
			client := testClient(t, func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "/"+resource) {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("failed"))}, nil
				}
				return jsonPage(t, []catalogProduct{}, 1, 1), nil
			})
			got, err := client.Fetch(t.Context(), AllCategories)
			require.ErrorContains(t, err, resource+" page 1")
			assert.Nil(t, got)
		})
	}
}

func TestRateLimitRetries(t *testing.T) {
	for _, succeed := range []bool{true, false} {
		t.Run(fmt.Sprint(succeed), func(t *testing.T) {
			calls := 0
			var bodies []*trackedBody
			client := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if succeed && calls == 3 {
					return jsonPage(t, []catalogProduct{}, 1, 0), nil
				}
				body := &trackedBody{Reader: strings.NewReader("rate limited")}
				bodies = append(bodies, body)
				return &http.Response{StatusCode: 429, Header: http.Header{"X-Rate-Limit-Time-Reset-Ms": []string{"0"}}, Body: body}, nil
			})
			var page catalogPage[catalogProduct]
			err := client.getPage(t.Context(), "https://api.bigcommerce.com/stores/test/v3/catalog/products", &page)
			if succeed {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "HTTP 429")
			}
			assert.Equal(t, 3, calls)
			for _, body := range bodies {
				assert.True(t, body.closed)
			}
		})
	}
}

func TestFetchCancellation(t *testing.T) {
	for _, rateLimited := range []bool{false, true} {
		t.Run(fmt.Sprint(rateLimited), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client := testClient(t, func(req *http.Request) (*http.Response, error) {
				cancel()
				if !rateLimited {
					return nil, req.Context().Err()
				}
				return &http.Response{StatusCode: 429, Header: http.Header{"X-Rate-Limit-Time-Reset-Ms": []string{"60000"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			got, err := client.Fetch(ctx, AllCategories)
			require.ErrorIs(t, err, context.Canceled)
			assert.Nil(t, got)
		})
	}
}
