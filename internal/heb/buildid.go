package heb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmlstd "html"
	"os"
	"strings"
	"time"

	"careme/internal/brightdata"
	"careme/internal/cache"

	"golang.org/x/net/html"
)

const (
	brightDataBrowserWSEnv     = "BRIGHTDATA_BROWSER_WS_ENDPOINT"
	defaultBuildIDDiscoverWait = 5 * time.Second
	BuildIDLatestCacheKey      = "heb/build_id/latest.json"
)

type browserHTMLClient interface {
	HTML(ctx context.Context, targetURL string, opts brightdata.BrowserOptions) (string, error)
}

// interface?
type loadBuildID func(context.Context) (string, error)

type BuildIDRecord struct {
	BuildID   string    `json:"build_id"`
	FetchedAt time.Time `json:"fetched_at"`
}

func newBrightDataBuildIDLoaderFromEnv() (loadBuildID, error) {
	hebCache, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create heb cache: %w", err)
	}

	// move to config?
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

	return func(ctx context.Context) (string, error) {
		buildID, err := fetchBuildIDFromHomePage(ctx, browser, defaultBuildIDDiscoverWait)
		if err != nil {
			return "", err
		}
		if err := saveLatestBuildID(ctx, hebCache, buildID); err != nil {
			return "", err
		}
		return buildID, nil
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

func saveLatestBuildID(ctx context.Context, c cache.Cache, buildID string) error {
	if c == nil {
		return errors.New("cache is required")
	}
	buildID = strings.TrimSpace(buildID)
	if buildID == "" {
		return errors.New("build id is required")
	}

	record := BuildIDRecord{
		BuildID:   buildID,
		FetchedAt: time.Now().UTC(),
	}
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal heb build id record: %w", err)
	}
	if err := c.Put(ctx, BuildIDLatestCacheKey, string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("write heb build id cache: %w", err)
	}
	return nil
}

func loadLatestBuildID(ctx context.Context, c cache.Cache) (string, error) {
	if c == nil {
		return "", errors.New("cache is required")
	}

	reader, err := c.Get(ctx, BuildIDLatestCacheKey)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = reader.Close()
	}()

	var record BuildIDRecord
	if err := json.NewDecoder(reader).Decode(&record); err != nil {
		return "", fmt.Errorf("decode heb build id record: %w", err)
	}
	buildID := strings.TrimSpace(record.BuildID)
	if buildID == "" {
		return "", fmt.Errorf("decode heb build id record: build id is empty")
	}
	return buildID, nil
}

func extractBuildID(body []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("parse category page html: %w", err)
	}

	var script string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if script != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "script" && queryAttrValue(n, "id") == "__NEXT_DATA__" {
			if n.FirstChild != nil {
				script = n.FirstChild.Data
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	if strings.TrimSpace(script) != "" {
		var data nextData
		if err := json.Unmarshal([]byte(htmlstd.UnescapeString(script)), &data); err != nil {
			return "", fmt.Errorf("decode next data json: %w", err)
		}
		if strings.TrimSpace(data.BuildID) != "" {
			return strings.TrimSpace(data.BuildID), nil
		}
	}

	matches := nextStaticBuildIDRe.FindSubmatch(body)
	if len(matches) == 2 && strings.TrimSpace(string(matches[1])) != "" {
		return strings.TrimSpace(string(matches[1])), nil
	}

	matches = nextDataBuildIDRe.FindSubmatch(body)
	if len(matches) == 2 && strings.TrimSpace(string(matches[1])) != "" {
		return strings.TrimSpace(string(matches[1])), nil
	}

	return "", errors.New("next data build id not found")
}
