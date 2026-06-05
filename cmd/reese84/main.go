package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"careme/internal/albertsons"
	"careme/internal/brightdata"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/logsetup"
)

const (
	defaultCookieName      = "reese84"
	brightDataBrowserWSEnv = "BRIGHTDATA_BROWSER_WS_ENDPOINT"
)

var defaultSiteURLs = map[string]string{
	"albertsons": "https://www.acmemarkets.com/aisle-vs/meat-seafood/seafood-favorites.html",
	"heb":        "https://www.heb.com/category/shop/meat-seafood/seafood/490023/490111",
}

type cookieRecord struct {
	Cookie    string     `json:"cookie"`
	FetchedAt time.Time  `json:"fetched_at"`
	SourceURL string     `json:"source_url"`
	Provider  string     `json:"provider"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
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
	fs := flag.NewFlagSet("reese84", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		site       string
		container  string
		targetURL  string
		cookieName string
		wsEndpoint string
		waitMS     int
		timeoutSec int
	)

	if err := config.LoadEncryptedEnv("secrets/envtest"); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fs.StringVar(&site, "site", "", "site name used for reese84 cache keys")
	fs.StringVar(&container, "container", "", "cache container name")
	fs.StringVar(&targetURL, "url", "", "page to navigate before reading cookies")
	fs.StringVar(&cookieName, "cookie-name", defaultCookieName, "cookie name to store")
	fs.StringVar(&wsEndpoint, "ws-endpoint", strings.TrimSpace(os.Getenv(brightDataBrowserWSEnv)), "Bright Data Browser API websocket endpoint including credentials")
	fs.IntVar(&waitMS, "wait-ms", int((5*time.Second)/time.Millisecond), "wait after initial navigation before reading cookies")
	fs.IntVar(&timeoutSec, "timeout", 120, "overall timeout in seconds")

	if err := fs.Parse(args); err != nil {
		return err
	}

	site = strings.TrimSpace(site)
	if site == "" {
		return errors.New("site is required")
	}
	if !validCacheName(site) {
		return fmt.Errorf("invalid site %q", site)
	}

	container = strings.TrimSpace(container)
	if container == "" {
		return errors.New("container is required")
	}
	if !validCacheName(container) {
		return fmt.Errorf("invalid container %q", container)
	}

	targetURL = strings.TrimSpace(targetURL)
	if targetURL == "" {
		defaultURL, ok := defaultSiteURLs[site]
		if !ok {
			return fmt.Errorf("url is required for site %q", site)
		}
		targetURL = defaultURL
	}
	if strings.TrimSpace(cookieName) == "" {
		return errors.New("cookie-name is required")
	}
	wsEndpoint = strings.TrimSpace(wsEndpoint)
	if wsEndpoint == "" {
		return fmt.Errorf("%s is required", brightDataBrowserWSEnv)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	browser, err := brightdata.NewBrowserClient(brightdata.BrowserClientConfig{
		WSEndpoint: wsEndpoint,
	})
	if err != nil {
		return fmt.Errorf("create Bright Data browser client: %w", err)
	}

	record, err := albertsons.FetchCookie(fetchCtx, browser, albertsons.CookieParams{
		TargetURL:           targetURL,
		CookieName:          cookieName,
		WaitAfterNavigation: time.Duration(waitMS) * time.Millisecond,
	})
	if err != nil {
		return err
	}

	cacheStore, err := cache.EnsureCache(container)
	if err != nil {
		return fmt.Errorf("create %s cache: %w", container, err)
	}

	if err := saveReese84Record(fetchCtx, cacheStore, site, cookieRecord{
		Cookie:    record.Cookie,
		FetchedAt: record.FetchedAt,
		SourceURL: record.SourceURL,
		Provider:  record.Provider,
		ExpiresAt: record.ExpiresAt,
	}); err != nil {
		return fmt.Errorf("cache %s reese84 cookie: %w", site, err)
	}

	fmt.Printf("cached %s for %s at %s from %s in %s cache\n", cookieName, site, record.FetchedAt.Format(time.RFC3339), targetURL, container)
	return nil
}

func saveReese84Record(ctx context.Context, c cache.Cache, site string, record cookieRecord) error {
	if c == nil {
		return errors.New("cache is required")
	}
	if !validCacheName(site) {
		return fmt.Errorf("invalid site %q", site)
	}

	record.Cookie = strings.TrimSpace(record.Cookie)
	if record.Cookie == "" {
		return errors.New("cookie is required")
	}

	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal reese84 record: %w", err)
	}

	historyKey := path.Join(site, "reese84", "history", record.FetchedAt.Format(time.RFC3339Nano)+".json")
	if err := c.Put(ctx, historyKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write reese84 history: %w", err)
	}
	latestKey := path.Join(site, "reese84", "latest.json")
	if err := c.Put(ctx, latestKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write reese84 latest: %w", err)
	}
	return nil
}

func validCacheName(value string) bool {
	if value == "" || strings.Contains(value, "/") || strings.Contains(value, "..") {
		return false
	}
	return value == path.Clean(value)
}
