package heb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"careme/internal/brightdata"
	"careme/internal/cache"
)

type stubHTMLClient struct {
	body      string
	err       error
	targetURL string
	opts      brightdata.BrowserOptions
}

func (s *stubHTMLClient) HTML(_ context.Context, targetURL string, opts brightdata.BrowserOptions) (string, error) {
	s.targetURL = targetURL
	s.opts = opts
	return s.body, s.err
}

func TestFetchBuildIDFromHomePage(t *testing.T) {
	t.Parallel()

	browser := &stubHTMLClient{
		body: `<!doctype html><html><body><script id="__NEXT_DATA__" type="application/json">{"buildId":"fresh-build"}</script></body></html>`,
	}

	got, err := fetchBuildIDFromHomePage(t.Context(), browser, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("fetchBuildIDFromHomePage returned error: %v", err)
	}
	if got != "fresh-build" {
		t.Fatalf("unexpected build id: got %q want %q", got, "fresh-build")
	}
	if browser.targetURL != DefaultBaseURL+"/" {
		t.Fatalf("unexpected target URL: %q", browser.targetURL)
	}
	if browser.opts.WaitAfterNavigation != 250*time.Millisecond {
		t.Fatalf("unexpected wait: %s", browser.opts.WaitAfterNavigation)
	}
}

func TestFetchBuildIDFromHomePageReturnsBrowserError(t *testing.T) {
	t.Parallel()

	_, err := fetchBuildIDFromHomePage(t.Context(), &stubHTMLClient{err: errors.New("browser failed")}, time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewBrightDataBuildIDLoaderFromEnvRequiresEndpoint(t *testing.T) {
	t.Setenv(brightDataBrowserWSEnv, "")

	_, err := newBrightDataBuildIDLoaderFromEnv()
	if err == nil || !strings.Contains(err.Error(), brightDataBrowserWSEnv) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveAndLoadLatestBuildID(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	if err := saveLatestBuildID(t.Context(), cacheStore, " fresh-build "); err != nil {
		t.Fatalf("SaveLatestBuildID returned error: %v", err)
	}

	got, err := loadLatestBuildID(t.Context(), cacheStore)
	if err != nil {
		t.Fatalf("LoadLatestBuildID returned error: %v", err)
	}
	if got != "fresh-build" {
		t.Fatalf("unexpected build id: got %q want %q", got, "fresh-build")
	}
}

func TestExtractBuildIDFromNextStaticAsset(t *testing.T) {
	t.Parallel()

	buildID, err := extractBuildID([]byte(`<!doctype html><html><head><script src="/_next/static/static-build-id/_buildManifest.js"></script></head></html>`))
	if err != nil {
		t.Fatalf("extractBuildID returned error: %v", err)
	}
	if buildID != "static-build-id" {
		t.Fatalf("unexpected build id: %q", buildID)
	}
}

func TestExtractBuildIDFromNextDataURL(t *testing.T) {
	t.Parallel()

	buildID, err := extractBuildID([]byte(`window.__SSR_URL__ = "/_next/data/data-build-id/en/category/shop/490110/490529.json?childId=490529&page=1&parentId=490110"`))
	if err != nil {
		t.Fatalf("extractBuildID returned error: %v", err)
	}
	if buildID != "data-build-id" {
		t.Fatalf("unexpected build id: %q", buildID)
	}
}

func TestExtractBuildIDErrorsWhenMissing(t *testing.T) {
	t.Parallel()

	_, err := extractBuildID([]byte(`<!doctype html><html><body></body></html>`))
	if err == nil || !errors.Is(err, errors.New("next data build id not found")) && !strings.Contains(err.Error(), "next data build id not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}
