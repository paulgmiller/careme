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
	)

	fs.StringVar(&targetURL, "url", defaultTargetURL, "page to navigate before reading cookies")
	fs.StringVar(&cookieName, "cookie-name", defaultCookieName, "cookie name to store")
	fs.StringVar(&wsEndpoint, "ws-endpoint", strings.TrimSpace(os.Getenv(brightDataBrowserWSEnv)), "Bright Data Browser API websocket endpoint including credentials")
	fs.IntVar(&waitMS, "wait-ms", int((5*time.Second)/time.Millisecond), "wait after initial navigation before reading cookies")
	fs.IntVar(&timeoutSec, "timeout", 120, "overall timeout in seconds")

	if err := fs.Parse(args); err != nil {
		return err
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

	cacheStore, err := cache.EnsureCache(albertsons.Container)
	if err != nil {
		return fmt.Errorf("create albertsons cache: %w", err)
	}

	if err := albertsons.SaveReese84Record(fetchCtx, cacheStore, record); err != nil {
		return fmt.Errorf("cache reese84 cookie: %w", err)
	}

	fmt.Printf("cached %s at %s from %s\n", cookieName, record.FetchedAt.Format(time.RFC3339), targetURL)
	return nil
}
