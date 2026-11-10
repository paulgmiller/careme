package query

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSearchClientRequiresSubscriptionKey(t *testing.T) {
	t.Parallel()

	_, err := NewSearchClient(SearchClientConfig{})
	require.ErrorContains(t, err, "subscription key is required")
}

func TestSearchBuildsExpectedRequest(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client, err := NewSearchClient(SearchClientConfig{
		BaseURL:         "https://www.acmemarkets.com",
		SubscriptionKey: "test-subscription-key",
		Reese84Provider: func(context.Context) (string, error) { return "reese-cookie", nil },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				capturedReq = r
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					// this is going to fail
					Body: io.NopCloser(strings.NewReader(`{"response":{"numFound":3,"disableTracking":false,"start":0,"miscInfo":{"attributionToken":"","query":"","sort":"","filter":"","nextPageToken":""},"isExactMatch":true,"docs":[{"id":"1","name":"Apples","price":1.99},{"id":"2","name":"Bananas","price":2.49},{"id":"3","name":"Carrots","price":3.99}]}}`)),
				}, nil
			}),
		},
	})
	require.NoError(t, err)

	payload, err := client.search(context.Background(), "806", Category_Vegatables, SearchOptions{})
	require.NoError(t, err)

	require.NotNil(t, capturedReq)
	assert.Equal(t, defaultSearchPath, capturedReq.URL.Path)
	assert.Equal(t, 3, payload.Response.NumFound)

	query := capturedReq.URL.Query()
	assertQueryValue(t, query, "url", "https://www.acmemarkets.com")
	assertQueryValue(t, query, "q", "")
	assertQueryValue(t, query, "rows", "60")
	assertQueryValue(t, query, "start", "0")
	assertQueryValue(t, query, "channel", "instore")
	assertQueryValue(t, query, "storeid", "806")
	assertQueryValue(t, query, "sort", "")
	assertQueryValue(t, query, "widget-id", Category_Vegatables)

	assert.Equal(t, "application/json, text/plain, */*", capturedReq.Header.Get("Accept"))
	assert.Equal(t, "en-US,en;q=0.9", capturedReq.Header.Get("Accept-Language"))
	assert.Equal(t, "test-subscription-key", capturedReq.Header.Get("Ocp-Apim-Subscription-Key"))

	reese84Cookie, err := capturedReq.Cookie("reese84")
	require.NoError(t, err)
	assert.Equal(t, "reese-cookie", reese84Cookie.Value)
}

func TestSearchInfersSafewayBannerByDefault(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client, err := NewSearchClient(SearchClientConfig{
		SubscriptionKey: "test-subscription-key",
		Reese84Provider: func(context.Context) (string, error) { return "test-reese84", nil },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				capturedReq = r
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			}),
		},
	})
	require.NoError(t, err)

	_, err = client.search(context.Background(), "1444", Category_Vegatables, SearchOptions{})
	require.NoError(t, err)

	require.NotNil(t, capturedReq)
	assert.Equal(t, DefaultSearchBaseURL, capturedReq.URL.Query().Get("url"))
}

func TestSearchUsesReese84ProviderWhenConfigured(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client, err := NewSearchClient(SearchClientConfig{
		SubscriptionKey: "test-subscription-key",
		Reese84Provider: func(context.Context) (string, error) {
			return "fresh-cookie", nil
		},
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				capturedReq = r
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			}),
		},
	})
	require.NoError(t, err)

	_, err = client.search(context.Background(), "806", Category_Vegatables, SearchOptions{})
	require.NoError(t, err)

	require.NotNil(t, capturedReq)
	reese84Cookie, err := capturedReq.Cookie("reese84")
	require.NoError(t, err)
	assert.Equal(t, "fresh-cookie", reese84Cookie.Value)
}

func TestSearchReturnsProviderError(t *testing.T) {
	t.Parallel()

	client, err := NewSearchClient(SearchClientConfig{
		SubscriptionKey: "test-subscription-key",
		Reese84Provider: func(context.Context) (string, error) {
			return "", errors.New("boom")
		},
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				assert.Fail(t, "unexpected HTTP call")
				return nil, errors.New("unexpected HTTP call")
			}),
		},
	})
	require.NoError(t, err)

	_, err = client.search(context.Background(), "806", Category_Vegatables, SearchOptions{})
	require.ErrorContains(t, err, "resolve reese84")
}

func TestSearch_RetriesTransient5xx(t *testing.T) {
	t.Parallel()

	attempts := 0
	client, err := NewSearchClient(SearchClientConfig{
		BaseURL:         "https://www.acmemarkets.com",
		SubscriptionKey: "test-subscription-key",
		Reese84Provider: func(context.Context) (string, error) { return "reese-cookie", nil },
		HTTPClient: retryingHTTPClient(&http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				attempts++
				if attempts < 3 {
					return &http.Response{
						StatusCode: http.StatusBadGateway,
						Body:       io.NopCloser(strings.NewReader("temporary failure")),
						Request:    r,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body:    io.NopCloser(strings.NewReader(`{"response":{"numFound":1,"disableTracking":false,"start":0,"miscInfo":{"attributionToken":"","query":"","sort":"","filter":"","nextPageToken":""},"isExactMatch":true,"docs":[{"id":"1","name":"Apples","price":1.99}]}}`)),
					Request: r,
				}, nil
			}),
		}),
	})
	require.NoError(t, err)

	payload, err := client.search(context.Background(), "806", Category_Vegatables, SearchOptions{})
	require.NoError(t, err)
	assert.Equal(t, 3, attempts)
	assert.Equal(t, 1, payload.Response.NumFound)
}

func TestSearchAllPaginatesUntilRequestedRows(t *testing.T) {
	t.Parallel()

	var starts []string
	client, err := NewSearchClient(SearchClientConfig{
		BaseURL:         "https://www.acmemarkets.com",
		SubscriptionKey: "test-subscription-key",
		Reese84Provider: func(context.Context) (string, error) { return "reese-cookie", nil },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				query := r.URL.Query()
				starts = append(starts, query.Get("start"))
				docs := make([]string, 60)
				for i := range docs {
					docs[i] = fmt.Sprintf(`{"id":"%d","name":"Product %d"}`, i, i)
				}
				if query.Get("start") == "120" {
					docs = docs[:5]
				}
				body := fmt.Sprintf(`{"response":{"numFound":125,"start":%s,"docs":[%s]}}`, query.Get("start"), strings.Join(docs, ","))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			}),
		},
	})
	require.NoError(t, err)

	products, err := client.SearchAll(context.Background(), "806", Category_Vegatables, SearchOptions{Rows: 125})
	require.NoError(t, err)
	assert.Len(t, products, 125)
	assert.Equal(t, []string{"0", "60", "120"}, starts)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func assertQueryValue(t *testing.T, values url.Values, key, want string) {
	t.Helper()
	assert.Equal(t, want, values.Get(key), "unexpected %s", key)
}

func retryingHTTPClient(base *http.Client) *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil
	retryClient.HTTPClient = base
	retryClient.RetryMax = 2
	retryClient.RetryWaitMin = time.Millisecond
	retryClient.RetryWaitMax = time.Millisecond
	retryClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if err != nil {
			return false, err
		}
		if resp == nil || resp.Request == nil {
			return false, nil
		}
		return resp.Request.Method == http.MethodGet && resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode <= 599, nil
	}
	return retryClient.StandardClient()
}
