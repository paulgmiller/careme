package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"careme/internal/albertsons"
	"careme/internal/brightdata"
	"careme/internal/cache"
	"careme/internal/logsetup"
)

const (
	// should we rotate between chains?
	defaultTargetURL       = "https://www.acmemarkets.com/aisle-vs/meat-seafood/seafood-favorites.html"
	defaultCookieName      = "reese84"
	brightDataBrowserWSEnv = "BRIGHTDATA_BROWSER_WS_ENDPOINT"
)

type browserCookieFetcher interface {
	Cookies(ctx context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error)
}

func main() {
	ctx := context.Background()
	closeLogger, err := logsetup.Configure(ctx)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeLogger()

	if err := runWithDeps(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runWithDeps(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("albertsonsreese84", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		targetURL  string
		cookieName string
		wsEndpoint string
		waitMS     int
		timeoutSec int
		ttlHours   int
	)

	fs.StringVar(&targetURL, "url", defaultTargetURL, "page to navigate before reading cookies")
	fs.StringVar(&cookieName, "cookie-name", defaultCookieName, "cookie name to store")
	fs.StringVar(&wsEndpoint, "ws-endpoint", strings.TrimSpace(os.Getenv(brightDataBrowserWSEnv)), "Bright Data Browser API websocket endpoint including credentials")
	fs.IntVar(&waitMS, "wait-ms", int((5*time.Second)/time.Millisecond), "wait after initial navigation before reading cookies")
	fs.IntVar(&timeoutSec, "timeout", 120, "overall timeout in seconds")
	fs.IntVar(&ttlHours, "ttl-hours", int(albertsons.DefaultReese84MaxAge/time.Hour), "freshness window recorded with the cookie")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(cookieName) == "" {
		return errors.New("cookie-name is required")
	}
	if ttlHours <= 0 {
		return errors.New("ttl-hours must be positive")
	}
	wsEndpoint = strings.TrimSpace(wsEndpoint)
	if wsEndpoint == "" {
		return fmt.Errorf("%s is required", brightDataBrowserWSEnv)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cookieValue, expiresAt, provider, err := fetchCookie(fetchCtx, targetURL, cookieName, wsEndpoint, time.Duration(waitMS)*time.Millisecond)
	if err != nil {
		return err
	}

	cacheStore, err := cache.EnsureCache(albertsons.Container)
	if err != nil {
		return fmt.Errorf("create albertsons cache: %w", err)
	}

	fetchedAt := time.Now().UTC()
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

	fmt.Printf("cached %s at %s from %s\n", cookieName, fetchedAt.Format(time.RFC3339), targetURL)
	return nil
}

func fetchCookie(ctx context.Context, targetURL, cookieName, wsEndpoint string, browserWait time.Duration) (string, *time.Time, string, error) {
	if wsEndpoint == "" {
		return "", nil, "", fmt.Errorf("%s is required", brightDataBrowserWSEnv)
	}

	browser, err := brightdata.NewBrowserClient(brightdata.BrowserClientConfig{
		WSEndpoint: wsEndpoint,
	})
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
