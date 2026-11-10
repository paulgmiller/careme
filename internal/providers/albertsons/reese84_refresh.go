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

func FetchCookie(ctx context.Context, browser cookieBrowser, params CookieParams) (cookieRecord, error) {
	if browser == nil {
		return cookieRecord{}, errors.New("browser is required")
	}

	params.TargetURL = strings.TrimSpace(params.TargetURL)
	if params.TargetURL == "" {
		return cookieRecord{}, errors.New("target URL is required")
	}

	params.CookieName = strings.TrimSpace(params.CookieName)
	if params.CookieName == "" {
		return cookieRecord{}, errors.New("cookie name is required")
	}

	cookies, err := browser.Cookies(ctx, params.TargetURL, brightdata.BrowserOptions{
		WaitAfterNavigation: params.WaitAfterNavigation,
	})
	if err != nil {
		return cookieRecord{}, fmt.Errorf("browser cookie fetch: %w", err)
	}

	cookie, ok := brightdata.CookieNamed(cookies, params.CookieName)
	if !ok {
		return cookieRecord{}, fmt.Errorf("cookie %q not found in browser session", params.CookieName)
	}

	record := cookieRecord{
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
