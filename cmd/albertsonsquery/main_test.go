package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestRunOutputsReturnedDocCount(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/abs/pub/xapi/wcax/pathway/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("storeid"); got != "806" {
			t.Fatalf("unexpected storeid: %q", got)
		}
		if got := r.URL.Query().Get("banner"); got != "acmemarkets" {
			t.Fatalf("unexpected banner: %q", got)
		}
		if got := r.Header.Get("Ocp-Apim-Subscription-Key"); got != "test-key" {
			t.Fatalf("unexpected subscription key: %q", got)
		}

		cookie, err := r.Cookie("ACI_S_abs_previouslogin")
		if err != nil {
			t.Fatalf("expected previous login cookie: %v", err)
		}
		if _, err := url.QueryUnescape(cookie.Value); err != nil {
			t.Fatalf("failed to decode cookie value: %v", err)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{
			"response":{"numFound":3,"disableTracking":false,"start":0,"miscInfo":{"attributionToken":"","query":"","sort":"","filter":"","nextPageToken":""},"isExactMatch":true,"docs":[{"id":"1"},{"id":"2"},{"id":"3"}]},
			"offersData":{"departments":{},"upcs":{}},
			"facet":{"ranges":[],"fields":[],"dynamic_facets":[]},
			"appCode":"ok",
			"appMsg":"ok",
			"dynamic_filters":{}
		}`)),
		}, nil
	})}

	var stdout strings.Builder
	err := runWithHTTPClient(context.Background(), &stdout, []string{
		"-base-url", "https://www.acmemarkets.com",
		"-banner", "acmemarkets",
		"-store-id", "806",
		"-zip", "19711",
		"-subscription-key", "test-key",
	}, httpClient)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if got := stdout.String(); got != "3\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
