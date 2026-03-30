package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
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
}

func (s *stubBrowserFetcher) Cookies(_ context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error) {
	s.targetURL = targetURL
	s.wait = opts.WaitAfterNavigation
	return s.cookies, nil
}

type stubUnlocker struct {
	resp *brightdata.UnlockerResponse
	err  error
}

func (s *stubUnlocker) Request(context.Context, brightdata.UnlockerRequest) (*brightdata.UnlockerResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func TestRunWithDepsCachesLatestCookieFromUnlocker(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetchedAt := time.Date(2026, time.March, 28, 13, 0, 0, 0, time.UTC)

	var stdout bytes.Buffer
	err := runWithDeps(context.Background(), &stdout, []string{
		"-api-key", "api-key",
		"-zone", "albertsons",
	}, dependencies{
		newUnlocker: func(apiKey string) (unlockerRequester, error) {
			if apiKey != "api-key" {
				t.Fatalf("unexpected api key: %q", apiKey)
			}
			return &stubUnlocker{
				resp: &brightdata.UnlockerResponse{
					StatusCode: 200,
					Headers: brightdata.UnlockerHeaders{
						"Set-Cookie": []string{"reese84=cookie-value; Path=/; HttpOnly"},
					},
				},
			}, nil
		},
		newBrowser: func(auth, wsEndpoint string) (browserCookieFetcher, error) {
			t.Fatalf("unexpected browser fallback")
			return nil, nil
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
	if record.Cookie != "cookie-value" {
		t.Fatalf("unexpected cached cookie: %q", record.Cookie)
	}
	if record.Provider != "brightdata-unlocker-api" {
		t.Fatalf("unexpected provider: %q", record.Provider)
	}
	if !strings.Contains(stdout.String(), "cached reese84") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunWithDepsFallsBackToBrowserWhenUnlockerMissesCookie(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetchedAt := time.Date(2026, time.March, 28, 14, 0, 0, 0, time.UTC)
	fetcher := &stubBrowserFetcher{
		cookies: []brightdata.BrowserCookie{
			{Name: "reese84", Value: "browser-cookie"},
		},
	}

	err := runWithDeps(context.Background(), &bytes.Buffer{}, []string{
		"-api-key", "api-key",
		"-zone", "albertsons",
		"-auth", "user:pass",
		"-wait-ms", "2500",
	}, dependencies{
		newUnlocker: func(apiKey string) (unlockerRequester, error) {
			return &stubUnlocker{
				resp: &brightdata.UnlockerResponse{
					StatusCode: 200,
					Headers:    brightdata.UnlockerHeaders{},
				},
			}, nil
		},
		newBrowser: func(auth, wsEndpoint string) (browserCookieFetcher, error) {
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
		t.Fatalf("unexpected browser fallback inputs: %q %s", fetcher.targetURL, fetcher.wait)
	}
}

func TestRunWithDepsErrorsWhenUnlockerFailsWithoutFallback(t *testing.T) {
	t.Parallel()

	err := runWithDeps(context.Background(), &bytes.Buffer{}, []string{
		"-api-key", "api-key",
		"-zone", "albertsons",
	}, dependencies{
		newUnlocker: func(apiKey string) (unlockerRequester, error) {
			return &stubUnlocker{err: errors.New("boom")}, nil
		},
		newBrowser: func(auth, wsEndpoint string) (browserCookieFetcher, error) {
			t.Fatalf("unexpected browser fallback")
			return nil, nil
		},
		newCache: func() (cache.Cache, error) {
			t.Fatalf("unexpected cache creation")
			return nil, nil
		},
		now:    time.Now,
		getenv: func(string) string { return "" },
	})
	if err == nil || !strings.Contains(err.Error(), `unlocker request: boom`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCookieFromHeader(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Add("Set-Cookie", "other=x; Path=/")
	header.Add("Set-Cookie", "reese84=cookie-value; Path=/; HttpOnly")

	cookie, ok := cookieFromHeader(header, "reese84")
	if !ok || cookie.Value != "cookie-value" {
		t.Fatalf("unexpected cookie: %#v ok=%v", cookie, ok)
	}
}
