package publix

import (
	"context"
	"errors"
	"testing"
	"time"

	"careme/internal/brightdata"
	"careme/internal/cache"
)

type stubAbckBrowser struct {
	cookies   []brightdata.BrowserCookie
	targetURL string
	wait      time.Duration
	err       error
}

func (s *stubAbckBrowser) Cookies(_ context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error) {
	s.targetURL = targetURL
	s.wait = opts.WaitAfterNavigation
	if s.err != nil {
		return nil, s.err
	}
	return s.cookies, nil
}

func TestSaveAbckRecordWritesLatestAndHistory(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetchedAt := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)

	err := SaveAbckRecord(t.Context(), cacheStore, AbckRecord{
		Cookie:    "cookie-value",
		FetchedAt: fetchedAt,
		SourceURL: "https://www.publix.com/c/beef/163c7c04-5495-404e-81fc-34f71b241093",
		Provider:  brightDataBrowserSource,
	})
	if err != nil {
		t.Fatalf("SaveAbckRecord returned error: %v", err)
	}

	record, err := LoadLatestAbck(t.Context(), cacheStore)
	if err != nil {
		t.Fatalf("LoadLatestAbck returned error: %v", err)
	}
	if record.Cookie != "cookie-value" {
		t.Fatalf("unexpected cookie: %q", record.Cookie)
	}

	keys, err := cacheStore.List(t.Context(), AbckHistoryPrefix, "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(keys))
	}
	if got, want := keys[0], fetchedAt.Format(time.RFC3339Nano)+".json"; got != want {
		t.Fatalf("unexpected history key: got %q want %q", got, want)
	}
}

func TestFetchAbckReturnsRecord(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, time.June, 1, 22, 0, 0, 0, time.UTC)
	browser := &stubAbckBrowser{
		cookies: []brightdata.BrowserCookie{
			{Name: "_abck", Value: "cookie-value", Expires: &expiresAt},
		},
	}

	record, err := FetchAbck(context.Background(), browser, AbckParams{
		TargetURL:           "https://www.publix.com/c/beef/163c7c04-5495-404e-81fc-34f71b241093",
		WaitAfterNavigation: 2500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("FetchAbck returned error: %v", err)
	}

	if record.Cookie != "cookie-value" {
		t.Fatalf("unexpected cookie: %q", record.Cookie)
	}
	if record.Provider != brightDataBrowserSource {
		t.Fatalf("unexpected provider: %q", record.Provider)
	}
	if record.ExpiresAt == nil || !record.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected expiry: %+v", record.ExpiresAt)
	}
	if browser.targetURL != record.SourceURL {
		t.Fatalf("unexpected target URL: %q", browser.targetURL)
	}
	if browser.wait != 2500*time.Millisecond {
		t.Fatalf("unexpected wait: %s", browser.wait)
	}
}

func TestFetchAbckPropagatesBrowserError(t *testing.T) {
	t.Parallel()

	_, err := FetchAbck(context.Background(), &stubAbckBrowser{
		err: errors.New("boom"),
	}, AbckParams{
		TargetURL: "https://www.publix.com/c/beef/163c7c04-5495-404e-81fc-34f71b241093",
	})
	if err == nil || err.Error() != "browser cookie fetch: boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchAbckErrorsWhenCookieMissing(t *testing.T) {
	t.Parallel()

	_, err := FetchAbck(context.Background(), &stubAbckBrowser{
		cookies: []brightdata.BrowserCookie{{Name: "other", Value: "x"}},
	}, AbckParams{
		TargetURL: "https://www.publix.com/c/beef/163c7c04-5495-404e-81fc-34f71b241093",
	})
	if err == nil || err.Error() != `cookie "_abck" not found in browser session` {
		t.Fatalf("unexpected error: %v", err)
	}
}
