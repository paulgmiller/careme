package query

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNewSearchClientRequiresSubscriptionKey(t *testing.T) {
	t.Parallel()

	_, err := NewSearchClient(SearchClientConfig{})
	if err == nil || !strings.Contains(err.Error(), "subscription key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchBuildsExpectedRequest(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client, err := NewSearchClient(SearchClientConfig{
		BaseURL:         "https://www.acmemarkets.com",
		SubscriptionKey: "test-subscription-key",
		Reese84:         "reese-cookie",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				capturedReq = r
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					//this is going to fail
					Body: io.NopCloser(strings.NewReader(`{"response":{"numFound":3,"disableTracking":false,"start":0,"miscInfo":{"attributionToken":"","query":"","sort":"","filter":"","nextPageToken":""},"isExactMatch":true,"docs":[{"id":"1","name":"Apples","price":1.99},{"id":"2","name":"Bananas","price":2.49},{"id":"3","name":"Carrots","price":3.99}]}}`)),
				}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSearchClient returned error: %v", err)
	}

	payload, err := client.Search(context.Background(), "806", Category_Vegatables, SearchOptions{})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != defaultSearchPath {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if payload.Response.NumFound != 3 {
		t.Fatalf("expected 3 docs")
	}

	query := capturedReq.URL.Query()
	assertQueryValue(t, query, "url", "https://www.acmemarkets.com")
	assertQueryValue(t, query, "q", "")
	assertQueryValue(t, query, "rows", "60")
	assertQueryValue(t, query, "start", "0")
	assertQueryValue(t, query, "channel", "instore")
	assertQueryValue(t, query, "storeid", "806")
	assertQueryValue(t, query, "sort", "")
	assertQueryValue(t, query, "widget-id", Category_Vegatables)

	if got := capturedReq.Header.Get("Accept"); got != "application/json, text/plain, */*" {
		t.Fatalf("unexpected accept header: %q", got)
	}
	if got := capturedReq.Header.Get("Accept-Language"); got != "en-US,en;q=0.9" {
		t.Fatalf("unexpected accept-language header: %q", got)
	}
	if got := capturedReq.Header.Get("Ocp-Apim-Subscription-Key"); got != "test-subscription-key" {
		t.Fatalf("unexpected subscription header: %q", got)
	}

	reese84Cookie, err := capturedReq.Cookie("reese84")
	if err != nil {
		t.Fatalf("expected reese84 cookie: %v", err)
	}
	if reese84Cookie.Value != "reese-cookie" {
		t.Fatalf("unexpected reese84 cookie: %q", reese84Cookie.Value)
	}

}

func TestSearchInfersSafewayBannerByDefault(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client, err := NewSearchClient(SearchClientConfig{
		SubscriptionKey: "test-subscription-key",
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
	if err != nil {
		t.Fatalf("NewSearchClient returned error: %v", err)
	}

	if _, err := client.Search(context.Background(), "1444", Category_Vegatables, SearchOptions{}); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if got := capturedReq.URL.Query().Get("url"); got != DefaultSearchBaseURL {
		t.Fatalf("unexpected url query value: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func assertQueryValue(t *testing.T, values url.Values, key, want string) {
	t.Helper()
	if got := values.Get(key); got != want {
		t.Fatalf("unexpected %s: got %q want %q", key, got, want)
	}
}
