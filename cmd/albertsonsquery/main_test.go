package main

import (
	"context"
	"io"
	"net/http"
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
		if got := r.Header.Get("Ocp-Apim-Subscription-Key"); got != "test-key" {
			t.Fatalf("unexpected subscription key: %q", got)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
			},
			Body: io.NopCloser(strings.NewReader(`{
			"response":{"numFound":3,"disableTracking":false,"start":0,"miscInfo":{"attributionToken":"","query":"","sort":"","filter":"","nextPageToken":""},"isExactMatch":true,"docs":[{"id":"1","name":"Apples","price":1.99},{"id":"2","name":"Bananas","price":2.49},{"id":"3","name":"Carrots","price":3.99}]},
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
		"-store-id", "806",
		"-subscription-key", "test-key",
	}, httpClient)
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if got := stdout.String(); got != "1: Apples (price: 1.99)\n2: Bananas (price: 2.49)\n3: Carrots (price: 3.99)\ntotal products: 3\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
