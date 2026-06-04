package heb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"careme/internal/brightdata"
	"careme/internal/cache"
)

const (
	brightDataBrowserWSEnv     = "BRIGHTDATA_BROWSER_WS_ENDPOINT"
	nextDataBuildIDEnv         = "HEB_NEXT_DATA_BUILD_ID"
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

func DiscoverNextDataBuildIDFromEnv(ctx context.Context) (string, error) {
	loader, err := newBrightDataBuildIDLoaderFromEnv()
	if err != nil {
		return "", err
	}
	return loader(ctx, buildIDOptions{})
}

func CachedNextDataBuildIDProvider(c cache.Cache, provided string) func(context.Context) (string, error) {
	provided = strings.TrimSpace(provided)
	if provided == "" {
		provided = strings.TrimSpace(os.Getenv(nextDataBuildIDEnv))
	}

	return func(ctx context.Context) (string, error) {
		if provided != "" {
			if err := SaveNextDataBuildID(ctx, c, provided); err != nil {
				return "", err
			}
			return provided, nil
		}

		buildID, err := LoadNextDataBuildID(ctx, c)
		if err == nil {
			return buildID, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			return "", err
		}

		buildID, err = DiscoverNextDataBuildIDFromEnv(ctx)
		if err != nil {
			return "", err
		}
		if err := SaveNextDataBuildID(ctx, c, buildID); err != nil {
			return "", err
		}
		return buildID, nil
	}
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
