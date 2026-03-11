package sitemapfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchURLs(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://example.com/sitemap.xml" {
				return responseWithBody(http.StatusNotFound, "not found"), nil
			}
			return responseWithBody(http.StatusOK, `<?xml version="1.0" encoding="UTF-8"?><urlset><url><loc>https://example.com/store-1</loc></url><url><loc> https://example.com/store-2 </loc></url><url><loc></loc></url></urlset>`), nil
		}),
	}

	got, err := FetchURLs(context.Background(), client, "https://example.com/sitemap.xml")
	if err != nil {
		t.Fatalf("FetchURLs returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 urls, got %d", len(got))
	}
	if got[0] != "https://example.com/store-1" || got[1] != "https://example.com/store-2" {
		t.Fatalf("unexpected urls: %+v", got)
	}
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
