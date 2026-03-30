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
	defaultTargetURL            = "https://www.acmemarkets.com/aisle-vs/meat-seafood/seafood-favorites.html"
	defaultCookieName           = "reese84"
	brightDataBrowserAuthEnv    = "BRIGHTDATA_BROWSER_AUTH"
	brightDataBrowserWSEndpoint = "BRIGHTDATA_BROWSER_WS_ENDPOINT"
)

type cookieFetcher interface {
	Cookies(ctx context.Context, targetURL string, opts brightdata.BrowserOptions) ([]brightdata.BrowserCookie, error)
}

type dependencies struct {
	newFetcher func(auth, wsEndpoint string) (cookieFetcher, error)
	newCache   func() (cache.Cache, error)
	now        func() time.Time
	getenv     func(string) string
}

func main() {
	ctx := context.Background()
	closeLogger, err := logsetup.Configure(ctx)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeLogger()

	if err := runWithDeps(ctx, os.Stdout, os.Args[1:], dependencies{
		newFetcher: func(auth, wsEndpoint string) (cookieFetcher, error) {
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
		auth       string
		wsEndpoint string
		waitMS     int
		timeoutSec int
		ttlHours   int
	)

	fs.StringVar(&targetURL, "url", defaultTargetURL, "page to navigate before reading cookies")
	fs.StringVar(&cookieName, "cookie-name", defaultCookieName, "cookie name to store")
	fs.StringVar(&auth, "auth", strings.TrimSpace(deps.getenv(brightDataBrowserAuthEnv)), "Bright Data browser auth in USER:PASS format")
	fs.StringVar(&wsEndpoint, "ws-endpoint", strings.TrimSpace(deps.getenv(brightDataBrowserWSEndpoint)), "Bright Data browser websocket endpoint")
	fs.IntVar(&waitMS, "wait-ms", int((5*time.Second)/time.Millisecond), "wait after initial navigation before reading cookies")
	fs.IntVar(&timeoutSec, "timeout", 120, "overall timeout in seconds")
	fs.IntVar(&ttlHours, "ttl-hours", int(albertsons.DefaultReese84MaxAge/time.Hour), "freshness window recorded with the cookie")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if deps.newFetcher == nil {
		return errors.New("fetcher factory is required")
	}
	if deps.newCache == nil {
		return errors.New("cache factory is required")
	}
	if deps.now == nil {
		deps.now = time.Now
	}
	if strings.TrimSpace(auth) == "" {
		return fmt.Errorf("%s is required", brightDataBrowserAuthEnv)
	}
	if strings.TrimSpace(cookieName) == "" {
		return errors.New("cookie-name is required")
	}
	if ttlHours <= 0 {
		return errors.New("ttl-hours must be positive")
	}

	fetchCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	fetcher, err := deps.newFetcher(auth, wsEndpoint)
	if err != nil {
		return fmt.Errorf("create Bright Data browser client: %w", err)
	}
	cookies, err := fetcher.Cookies(fetchCtx, targetURL, brightdata.BrowserOptions{
		WaitAfterNavigation: time.Duration(waitMS) * time.Millisecond,
	})
	if err != nil {
		return fmt.Errorf("fetch cookies: %w", err)
	}

	cookie, ok := brightdata.CookieNamed(cookies, cookieName)
	if !ok {
		return fmt.Errorf("cookie %q not found after navigating to %s", cookieName, targetURL)
	}

	cacheStore, err := deps.newCache()
	if err != nil {
		return fmt.Errorf("create albertsons cache: %w", err)
	}

	fetchedAt := deps.now().UTC()
	if err := albertsons.SaveReese84Record(fetchCtx, cacheStore, albertsons.Reese84Record{
		Cookie:    cookie.Value,
		FetchedAt: fetchedAt,
		SourceURL: targetURL,
		Provider:  "brightdata-browser-api",
		TTLHours:  ttlHours,
		ExpiresAt: cookie.Expires,
	}); err != nil {
		return fmt.Errorf("cache reese84 cookie: %w", err)
	}

	_, err = fmt.Fprintf(stdout, "cached %s at %s from %s\n", cookieName, fetchedAt.Format(time.RFC3339), targetURL)
	return err
}
