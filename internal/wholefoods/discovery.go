package wholefoods

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

type sitemapURLSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

var storeIDRe = regexp.MustCompile(`store-id="(\d+)"`)

func FetchSitemap(ctx context.Context, client *http.Client, sitemapURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build sitemap request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get sitemap: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get sitemap: status %s", resp.Status)
	}

	var sitemap sitemapURLSet
	if err := xml.NewDecoder(resp.Body).Decode(&sitemap); err != nil {
		return nil, fmt.Errorf("decode sitemap: %w", err)
	}

	urls := make([]string, 0, len(sitemap.URLs))
	for _, item := range sitemap.URLs {
		if loc := strings.TrimSpace(item.Loc); loc != "" {
			urls = append(urls, loc)
		}
	}
	return urls, nil
}

func FetchStoreIDFromPage(ctx context.Context, client *http.Client, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("build store page request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get store page: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get store page: status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read store page: %w", err)
	}

	matches := storeIDRe.FindSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("store-id not found")
	}
	return string(matches[1]), nil
}
