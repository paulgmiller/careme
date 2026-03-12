package logs

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandleLogsPageRedirectsToLocalDatasette(t *testing.T) {
	t.Parallel()

	h := &handler{}
	req := httptest.NewRequest(http.MethodGet, "/admin/logs?hours=6", nil)
	rr := httptest.NewRecorder()
	h.handleLogsPage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, `new URL("/admin/datasette/", location.origin)`) {
		t.Fatalf("expected local datasette URL in body, got: %s", body)
	}
	if strings.Contains(body, "https://lite.datasette.io/") {
		t.Fatalf("expected no direct lite.datasette.io redirect, got: %s", body)
	}
}

func TestNewDatasetteProxy(t *testing.T) {
	t.Parallel()

	proxy, err := newDatasetteProxy()
	if err != nil {
		t.Fatalf("expected proxy without error, got: %v", err)
	}
	if proxy == nil {
		t.Fatal("expected non-nil proxy")
	}
}

func TestNewSingleHostProxyRewritesHost(t *testing.T) {
	t.Parallel()

	target, err := url.Parse("https://example.test/sub")
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}

	proxy := newSingleHostProxy(target)
	req := httptest.NewRequest(http.MethodGet, "http://localhost/admin/datasette/?json=http://localhost:8080/admin/api/logs", nil)
	proxy.Director(req)

	if req.Host != "example.test" {
		t.Fatalf("expected host example.test, got %s", req.Host)
	}
	if req.URL.Scheme != "https" {
		t.Fatalf("expected scheme https, got %s", req.URL.Scheme)
	}
	if req.URL.Host != "example.test" {
		t.Fatalf("expected url host example.test, got %s", req.URL.Host)
	}
	if req.URL.Path != "/sub/admin/datasette/" {
		t.Fatalf("expected path /sub/admin/datasette/, got %s", req.URL.Path)
	}
}
