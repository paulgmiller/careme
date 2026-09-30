package albertsons

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"careme/internal/brightdata"
)

type cookieBrowser interface {
	Cookies(ctx context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error)
}

type CookieParams struct {
	TargetURL           string
	CookieName          string
	WaitAfterNavigation time.Duration
}

func FetchCookie(ctx context.Context, browser cookieBrowser, params CookieParams) (CookieRecord, error) {
	if browser == nil {
		return CookieRecord{}, errors.New("browser is required")
	}

	params.TargetURL = strings.TrimSpace(params.TargetURL)
	if params.TargetURL == "" {
		return CookieRecord{}, errors.New("target URL is required")
	}

	params.CookieName = strings.TrimSpace(params.CookieName)
	if params.CookieName == "" {
		return CookieRecord{}, errors.New("cookie name is required")
	}

	cookies, err := browser.Cookies(ctx, params.TargetURL, brightdata.BrowserOptions{
		WaitAfterNavigation: params.WaitAfterNavigation,
	})
	if err != nil {
		return CookieRecord{}, fmt.Errorf("browser cookie fetch: %w", err)
	}

	cookie, ok := brightdata.CookieNamed(cookies, params.CookieName)
	if !ok {
		return CookieRecord{}, fmt.Errorf("cookie %q not found in browser session", params.CookieName)
	}

	record := CookieRecord{
		Cookie:    cookie.Value,
		FetchedAt: time.Now().UTC(),
		SourceURL: params.TargetURL,
		Provider:  brightDataBrowserSource,
	}
	if cookie.Expires != nil {
		// seems to be a month on inspection?
		expiresAt := *cookie.Expires
		record.ExpiresAt = &expiresAt
	}
	return record, nil
}
