package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"careme/internal/albertsons"
	"careme/internal/brightdata"
	"careme/internal/cache"
)

type stubBrowserFetcher struct {
	cookies   []brightdata.BrowserCookie
	targetURL string
	wait      time.Duration
	err       error
}

func (s *stubBrowserFetcher) Cookies(_ context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error) {
	s.targetURL = targetURL
	s.wait = opts.WaitAfterNavigation
	if s.err != nil {
		return nil, s.err
	}
	return s.cookies, nil
}

func TestRunWithDepsCachesLatestCookieFromBrowser(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetchedAt := time.Date(2026, time.March, 28, 13, 0, 0, 0, time.UTC)
	fetcher := &stubBrowserFetcher{
		cookies: []brightdata.BrowserCookie{
			{Name: "reese84", Value: "browser-cookie"},
		},
	}

	var stdout bytes.Buffer
	err := runWithDeps(context.Background(), &stdout, []string{
		"-ws-endpoint", "wss://user:pass@brd.superproxy.io:9222",
		"-wait-ms", "2500",
	}, dependencies{
		newBrowser: func(wsEndpoint string) (browserCookieFetcher, error) {
			if wsEndpoint != "wss://user:pass@brd.superproxy.io:9222" {
				t.Fatalf("unexpected websocket endpoint: %q", wsEndpoint)
			}
			return fetcher, nil
		},
		newCache: func() (cache.Cache, error) {
			return cacheStore, nil
		},
		now:    func() time.Time { return fetchedAt },
		getenv: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("runWithDeps returned error: %v", err)
	}

	record, err := albertsons.LoadLatestReese84(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("LoadLatestReese84 returned error: %v", err)
	}
	if record.Cookie != "browser-cookie" || record.Provider != "brightdata-browser-api" {
		t.Fatalf("unexpected record: %+v", record)
	}
	if fetcher.targetURL != defaultTargetURL || fetcher.wait != 2500*time.Millisecond {
		t.Fatalf("unexpected browser inputs: %q %s", fetcher.targetURL, fetcher.wait)
	}
	if !strings.Contains(stdout.String(), "cached reese84") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunWithDepsErrorsWhenBrowserFails(t *testing.T) {
	t.Parallel()

	err := runWithDeps(context.Background(), &bytes.Buffer{}, []string{
		"-ws-endpoint", "wss://user:pass@brd.superproxy.io:9222",
	}, dependencies{
		newBrowser: func(wsEndpoint string) (browserCookieFetcher, error) {
			return &stubBrowserFetcher{err: errors.New("boom")}, nil
		},
		newCache: func() (cache.Cache, error) {
			t.Fatalf("unexpected cache creation")
			return nil, nil
		},
		now:    time.Now,
		getenv: func(string) string { return "" },
	})
	if err == nil || !strings.Contains(err.Error(), `browser cookie fetch: boom`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWithDepsRequiresBrowserConfig(t *testing.T) {
	t.Parallel()

	err := runWithDeps(context.Background(), &bytes.Buffer{}, nil, dependencies{
		newBrowser: func(wsEndpoint string) (browserCookieFetcher, error) {
			t.Fatalf("unexpected browser creation")
			return nil, nil
		},
		newCache: func() (cache.Cache, error) {
			t.Fatalf("unexpected cache creation")
			return nil, nil
		},
		now:    time.Now,
		getenv: func(string) string { return "" },
	})
	if err == nil || !strings.Contains(err.Error(), brightDataBrowserWSEnv) {
		t.Fatalf("unexpected error: %v", err)
	}
}
