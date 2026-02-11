package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterStaticAssetsServesEmbeddedFiles(t *testing.T) {
	mux := http.NewServeMux()
	if err := registerStaticAssets(mux); err != nil {
		t.Fatalf("registerStaticAssets failed: %v", err)
	}

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/static/shoppinglist.js")
	if err != nil {
		t.Fatalf("GET static JS failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for static JS, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("expected javascript content-type, got %q", got)
	}
	if got := resp.Header.Get("ETag"); got == "" {
		t.Fatalf("expected ETag header for static asset")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed reading static JS response: %v", err)
	}
	if !strings.Contains(string(body), "updateFinalizeButton") {
		t.Fatalf("expected shoppinglist.js content in response body")
	}
}

func TestRegisterStaticAssetsReturnsNotModifiedWithMatchingETag(t *testing.T) {
	mux := http.NewServeMux()
	if err := registerStaticAssets(mux); err != nil {
		t.Fatalf("registerStaticAssets failed: %v", err)
	}

	server := httptest.NewServer(mux)
	defer server.Close()

	first, err := http.Get(server.URL + "/static/tailwind.css")
	if err != nil {
		t.Fatalf("initial GET failed: %v", err)
	}
	etag := first.Header.Get("ETag")
	_ = first.Body.Close()
	if etag == "" {
		t.Fatalf("expected ETag from initial response")
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/static/tailwind.css", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("If-None-Match", etag)
	second, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("conditional GET failed: %v", err)
	}
	defer func() { _ = second.Body.Close() }()

	if second.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304 for matching ETag, got %d", second.StatusCode)
	}
}
