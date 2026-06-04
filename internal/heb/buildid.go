package heb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"careme/internal/brightdata"
)

const (
	brightDataBrowserWSEnv     = "BRIGHTDATA_BROWSER_WS_ENDPOINT"
	defaultBuildIDDiscoverWait = 5 * time.Second
)

type browserHTMLClient interface {
	HTML(ctx context.Context, targetURL string, opts brightdata.BrowserOptions) (string, error)
}

type loadBuildID func(context.Context, buildIDOptions) (string, error)

type buildIDOptions struct {
	Reese84 string
	StoreID string
}

func newBrightDataBuildIDLoaderFromEnv() (loadBuildID, error) {
	wsEndpoint := strings.TrimSpace(os.Getenv(brightDataBrowserWSEnv))
	if wsEndpoint == "" {
		return nil, fmt.Errorf("%s is required for HEB build id discovery", brightDataBrowserWSEnv)
	}

	browser, err := brightdata.NewBrowserClient(brightdata.BrowserClientConfig{
		WSEndpoint: wsEndpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("create Bright Data browser client for HEB build id: %w", err)
	}

	return func(ctx context.Context, _ buildIDOptions) (string, error) {
		return fetchBuildIDFromHomePage(ctx, browser, defaultBuildIDDiscoverWait)
	}, nil
}

func fetchBuildIDFromHomePage(ctx context.Context, browser browserHTMLClient, wait time.Duration) (string, error) {
	body, err := browser.HTML(ctx, DefaultBaseURL+"/", brightdata.BrowserOptions{
		WaitAfterNavigation: wait,
	})
	if err != nil {
		return "", fmt.Errorf("fetch HEB homepage HTML with Bright Data browser: %w", err)
	}

	buildID, err := extractBuildID([]byte(body))
	if err != nil {
		return "", fmt.Errorf("extract HEB build id from homepage: %w", err)
	}
	return buildID, nil
}
