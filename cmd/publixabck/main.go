package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"careme/internal/brightdata"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/logsetup"
	"careme/internal/publix"
)

const (
	defaultTargetURL       = "https://www.publix.com/c/beef/163c7c04-5495-404e-81fc-34f71b241093"
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
	fs := flag.NewFlagSet("publixabck", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		targetURL  string
		wsEndpoint string
		waitMS     int
		timeoutSec int
	)

	_, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fs.StringVar(&targetURL, "url", defaultTargetURL, "page to navigate before reading cookies")
	fs.StringVar(&wsEndpoint, "ws-endpoint", strings.TrimSpace(os.Getenv(brightDataBrowserWSEnv)), "Bright Data Browser API websocket endpoint including credentials")
	fs.IntVar(&waitMS, "wait-ms", int((5*time.Second)/time.Millisecond), "wait after initial navigation before reading cookies")
	fs.IntVar(&timeoutSec, "timeout", 120, "overall timeout in seconds")

	if err := fs.Parse(args); err != nil {
		return err
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

	record, err := publix.FetchAbck(fetchCtx, browser, publix.AbckParams{
		TargetURL:           targetURL,
		WaitAfterNavigation: time.Duration(waitMS) * time.Millisecond,
	})
	if err != nil {
		return err
	}

	cacheStore, err := cache.EnsureCache(publix.Container)
	if err != nil {
		return fmt.Errorf("create publix cache: %w", err)
	}

	if err := publix.SaveAbckRecord(fetchCtx, cacheStore, record); err != nil {
		return fmt.Errorf("cache _abck cookie: %w", err)
	}

	fmt.Printf("cached _abck at %s from %s\n", record.FetchedAt.Format(time.RFC3339), targetURL)
	return nil
}
