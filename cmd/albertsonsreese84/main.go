package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"careme/internal/albertsons"
	"careme/internal/brightdata"
	"careme/internal/cache"
	"careme/internal/logsetup"
)

const (
	defaultTargetURL         = "https://www.acmemarkets.com/aisle-vs/meat-seafood/seafood-favorites.html"
	defaultCookieName        = "reese84"
	brightDataAPIKeyEnv      = "BRIGHTDATA_API_KEY"
	brightDataZoneEnv        = "BRIGHTDATA_ZONE"
	brightDataBrowserAuthEnv = "BRIGHTDATA_BROWSER_AUTH"
	brightDataBrowserWSEnv   = "BRIGHTDATA_BROWSER_WS_ENDPOINT"
)

type browserCookieFetcher interface {
	Cookies(ctx context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error)
}

type unlockerRequester interface {
	Request(ctx context.Context, req brightdata.UnlockerRequest) (*brightdata.UnlockerResponse, error)
}

type dependencies struct {
	newUnlocker func(apiKey string) (unlockerRequester, error)
	newBrowser  func(auth, wsEndpoint string) (browserCookieFetcher, error)
	newCache    func() (cache.Cache, error)
	now         func() time.Time
	getenv      func(string) string
}

func main() {
	ctx := context.Background()
	closeLogger, err := logsetup.Configure(ctx)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeLogger()

	if err := runWithDeps(ctx, os.Stdout, os.Args[1:], dependencies{
		newUnlocker: func(apiKey string) (unlockerRequester, error) {
			return brightdata.NewUnlockerClient(apiKey, &http.Client{Timeout: 60 * time.Second})
		},
		newBrowser: func(auth, wsEndpoint string) (browserCookieFetcher, error) {
			return brightdata.NewBrowserClient(brightdata.BrowserClientConfig{
				Auth:       auth,
				WSEndpoint: wsEndpoint,
			})
		},
		newCache: func() (cache.Cache, error) {
			return cache.EnsureCache(albertsons.Container)
		},
		now:    time.Now,
		getenv: os.Getenv,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runWithDeps(ctx context.Context, stdout io.Writer, args []string, deps dependencies) error {
	fs := flag.NewFlagSet("albertsonsreese84", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		targetURL  string
		cookieName string
		apiKey     string
		zone       string
		auth       string
		wsEndpoint string
		waitMS     int
		timeoutSec int
		ttlHours   int
	)

	fs.StringVar(&targetURL, "url", defaultTargetURL, "page to navigate before reading cookies")
	fs.StringVar(&cookieName, "cookie-name", defaultCookieName, "cookie name to store")
	fs.StringVar(&apiKey, "api-key", strings.TrimSpace(deps.getenv(brightDataAPIKeyEnv)), "Bright Data API key for Web Unlocker")
	fs.StringVar(&zone, "zone", strings.TrimSpace(deps.getenv(brightDataZoneEnv)), "Bright Data Web Unlocker zone")
	fs.StringVar(&auth, "auth", strings.TrimSpace(deps.getenv(brightDataBrowserAuthEnv)), "Bright Data Browser API auth in USER:PASS format")
	fs.StringVar(&wsEndpoint, "ws-endpoint", strings.TrimSpace(deps.getenv(brightDataBrowserWSEnv)), "Bright Data Browser API websocket endpoint")
	fs.IntVar(&waitMS, "wait-ms", int((5*time.Second)/time.Millisecond), "wait after initial navigation before reading cookies")
	fs.IntVar(&timeoutSec, "timeout", 120, "overall timeout in seconds")
	fs.IntVar(&ttlHours, "ttl-hours", int(albertsons.DefaultReese84MaxAge/time.Hour), "freshness window recorded with the cookie")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if deps.newUnlocker == nil {
		return errors.New("unlocker factory is required")
	}
	if deps.newBrowser == nil {
		return errors.New("browser factory is required")
	}
	if deps.newCache == nil {
		return errors.New("cache factory is required")
	}
	if deps.now == nil {
		deps.now = time.Now
	}
	if strings.TrimSpace(cookieName) == "" {
		return errors.New("cookie-name is required")
	}
	if ttlHours <= 0 {
		return errors.New("ttl-hours must be positive")
	}
	apiKey = strings.TrimSpace(apiKey)
	zone = strings.TrimSpace(zone)
	auth = strings.TrimSpace(auth)
	wsEndpoint = strings.TrimSpace(wsEndpoint)
	if apiKey == "" && auth == "" && wsEndpoint == "" {
		return fmt.Errorf("%s or %s/%s is required", brightDataAPIKeyEnv, brightDataBrowserAuthEnv, brightDataBrowserWSEnv)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cookieValue, expiresAt, provider, err := fetchCookie(fetchCtx, targetURL, cookieName, apiKey, zone, auth, wsEndpoint, time.Duration(waitMS)*time.Millisecond, deps)
	if err != nil {
		return err
	}

	cacheStore, err := deps.newCache()
	if err != nil {
		return fmt.Errorf("create albertsons cache: %w", err)
	}

	fetchedAt := deps.now().UTC()
	if err := albertsons.SaveReese84Record(fetchCtx, cacheStore, albertsons.Reese84Record{
		Cookie:    cookieValue,
		FetchedAt: fetchedAt,
		SourceURL: targetURL,
		Provider:  provider,
		TTLHours:  ttlHours,
		ExpiresAt: expiresAt,
	}); err != nil {
		return fmt.Errorf("cache reese84 cookie: %w", err)
	}

	_, err = fmt.Fprintf(stdout, "cached %s at %s from %s\n", cookieName, fetchedAt.Format(time.RFC3339), targetURL)
	return err
}

func fetchCookie(ctx context.Context, targetURL, cookieName, apiKey, zone, auth, wsEndpoint string,
	browserWait time.Duration, deps dependencies,
) (string, *time.Time, string, error) {
	if apiKey != "" {
		if zone == "" {
			return "", nil, "", fmt.Errorf("%s is required when %s is set", brightDataZoneEnv, brightDataAPIKeyEnv)
		}

		unlocker, err := deps.newUnlocker(apiKey)
		if err != nil {
			return "", nil, "", fmt.Errorf("create Bright Data unlocker client: %w", err)
		}

		resp, err := unlocker.Request(ctx, brightdata.UnlockerRequest{
			Zone:   zone,
			URL:    targetURL,
			Format: "json",
			Method: http.MethodGet,
		})
		if err != nil {
			return "", nil, "", fmt.Errorf("unlocker request: %w", err)
		}

		for key, values := range resp.Headers {
			log.Printf("unlocker response header: %s: %v", key, values)
		}

		cookie, ok := cookieFromHeader(resp.Headers.HTTPHeader(), cookieName)
		if ok {
			expires := cookie.Expires
			var expiresAt *time.Time
			if !expires.IsZero() {
				expiresAt = &expires
			}
			return cookie.Value, expiresAt, "brightdata-unlocker-api", nil
		}

		log.Printf("unlocker did not return %q; falling back to Bright Data Browser API", cookieName)
	}

	if auth == "" && wsEndpoint == "" {
		return "", nil, "", fmt.Errorf("cookie %q not found in unlocker response and %s is not configured for browser fallback", cookieName, brightDataBrowserAuthEnv)
	}

	browser, err := deps.newBrowser(auth, wsEndpoint)
	if err != nil {
		return "", nil, "", fmt.Errorf("create Bright Data browser client: %w", err)
	}

	cookies, err := browser.Cookies(ctx, targetURL, brightdata.BrowserOptions{
		WaitAfterNavigation: browserWait,
	})
	if err != nil {
		return "", nil, "", fmt.Errorf("browser cookie fetch: %w", err)
	}

	cookie, ok := brightdata.CookieNamed(cookies, cookieName)
	if !ok {
		return "", nil, "", fmt.Errorf("cookie %q not found in browser session", cookieName)
	}

	if cookie.Expires != nil {
		expiresAt := *cookie.Expires
		return cookie.Value, &expiresAt, "brightdata-browser-api", nil
	}
	return cookie.Value, nil, "brightdata-browser-api", nil
}

func cookieFromHeader(header http.Header, name string) (*http.Cookie, bool) {
	resp := &http.Response{Header: header}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name {
			return cookie, true
		}
	}
	return nil, false
}
