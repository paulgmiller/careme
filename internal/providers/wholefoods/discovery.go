package wholefoods

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"careme/internal/sitemapfetch"
)

var storeIDRe = regexp.MustCompile(`store-id="(\d+)"`)

func FetchSitemap(ctx context.Context, client *http.Client, sitemapURL string) ([]string, error) {
	return sitemapfetch.FetchURLs(ctx, client, sitemapURL)
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
