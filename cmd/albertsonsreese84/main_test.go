package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"careme/internal/albertsons"
	"careme/internal/brightdata"
	"careme/internal/cache"
)

type stubCookieFetcher struct {
	cookies   []brightdata.BrowserCookie
	targetURL string
	wait      time.Duration
}

func (s *stubCookieFetcher) Cookies(_ context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error) {
	s.targetURL = targetURL
	s.wait = opts.WaitAfterNavigation
	return s.cookies, nil
}

func TestRunWithDepsCachesLatestCookie(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetchedAt := time.Date(2026, time.March, 28, 13, 0, 0, 0, time.UTC)
	fetcher := &stubCookieFetcher{
		cookies: []brightdata.BrowserCookie{
			{Name: "other", Value: "ignore-me"},
			{Name: "reese84", Value: "cookie-value"},
		},
	}

	var stdout bytes.Buffer
	err := runWithDeps(context.Background(), &stdout, []string{
		"-auth", "user:pass",
		"-wait-ms", "2500",
	}, dependencies{
		newFetcher: func(auth, wsEndpoint string) (cookieFetcher, error) {
			if auth != "user:pass" {
				t.Fatalf("unexpected auth: %q", auth)
			}
			if wsEndpoint != "" {
				t.Fatalf("unexpected ws endpoint: %q", wsEndpoint)
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

	if fetcher.targetURL != defaultTargetURL {
		t.Fatalf("unexpected target URL: %q", fetcher.targetURL)
	}
	if fetcher.wait != 2500*time.Millisecond {
		t.Fatalf("unexpected wait: %s", fetcher.wait)
	}
	record, err := albertsons.LoadLatestReese84(context.Background(), cacheStore)
	if err != nil {
		t.Fatalf("LoadLatestReese84 returned error: %v", err)
	}
	if record.Cookie != "cookie-value" {
		t.Fatalf("unexpected cached cookie: %q", record.Cookie)
	}
	if !strings.Contains(stdout.String(), "cached reese84") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunWithDepsErrorsWhenCookieMissing(t *testing.T) {
	t.Parallel()

	err := runWithDeps(context.Background(), &bytes.Buffer{}, []string{
		"-auth", "user:pass",
	}, dependencies{
		newFetcher: func(auth, wsEndpoint string) (cookieFetcher, error) {
			return &stubCookieFetcher{cookies: []brightdata.BrowserCookie{
				{Name: "different", Value: "x"},
			}}, nil
		},
		newCache: func() (cache.Cache, error) {
			t.Fatalf("unexpected cache creation")
			return nil, nil
		},
		now:    time.Now,
		getenv: func(string) string { return "" },
	})
	if err == nil || !strings.Contains(err.Error(), `cookie "reese84" not found`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
