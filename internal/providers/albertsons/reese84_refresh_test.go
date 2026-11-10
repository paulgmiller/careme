package albertsons

import (
	"context"
	"errors"
	"testing"
	"time"

	"careme/internal/brightdata"
)

type stubReese84Browser struct {
	cookies   []brightdata.BrowserCookie
	targetURL string
	wait      time.Duration
	err       error
}

func (s *stubReese84Browser) Cookies(_ context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error) {
	s.targetURL = targetURL
	s.wait = opts.WaitAfterNavigation
	if s.err != nil {
		return nil, s.err
	}
	return s.cookies, nil
}

func TestFetchReese84RecordReturnsRecord(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, time.March, 30, 22, 0, 0, 0, time.UTC)
	browser := &stubReese84Browser{
		cookies: []brightdata.BrowserCookie{
			{Name: "reese84", Value: "cookie-value", Expires: &expiresAt},
		},
	}

	record, err := FetchCookie(context.Background(), browser, CookieParams{
		TargetURL:           "https://www.acmemarkets.com/aisle-vs/meat-seafood/seafood-favorites.html",
		CookieName:          "reese84",
		WaitAfterNavigation: 2500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("FetchReese84Record returned error: %v", err)
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

func TestFetchReese84RecordPropagatesBrowserError(t *testing.T) {
	t.Parallel()

	_, err := FetchCookie(context.Background(), &stubReese84Browser{
		err: errors.New("boom"),
	}, CookieParams{
		TargetURL:  "https://www.acmemarkets.com/aisle-vs/meat-seafood/seafood-favorites.html",
		CookieName: "reese84",
	})
	if err == nil || err.Error() != "browser cookie fetch: boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchReese84RecordErrorsWhenCookieMissing(t *testing.T) {
	t.Parallel()

	_, err := FetchCookie(context.Background(), &stubReese84Browser{
		cookies: []brightdata.BrowserCookie{{Name: "other", Value: "x"}},
	}, CookieParams{
		TargetURL:  "https://www.acmemarkets.com/aisle-vs/meat-seafood/seafood-favorites.html",
		CookieName: "reese84",
	})
	if err == nil || err.Error() != `cookie "reese84" not found in browser session` {
		t.Fatalf("unexpected error: %v", err)
	}
}
