package sitemapfetch

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
)

type urlSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

func FetchURLs(ctx context.Context, client *http.Client, sitemapURL string) ([]string, error) {
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

	var sitemap urlSet
	if err := xml.NewDecoder(resp.Body).Decode(&sitemap); err != nil {
		return nil, fmt.Errorf("decode sitemap: %w", err)
	}

	urls := make([]string, 0, len(sitemap.URLs))
	for _, item := range sitemap.URLs {
		loc := strings.TrimSpace(item.Loc)
		if loc == "" {
			continue
		}
		urls = append(urls, loc)
	}
	return urls, nil
}
