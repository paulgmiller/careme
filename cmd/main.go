package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const sitemapURL = "https://www.wholefoodsmarket.com/sitemap/sitemap-stores.xml"

type urlset struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

var storeIDRe = regexp.MustCompile(`store-id="(\d+)"`)

func main() {
	client := &http.Client{Timeout: 20 * time.Second}

	urls, err := fetchSitemap(client, sitemapURL)
	if err != nil {
		log.Fatal(err)
	}

	for _, u := range urls {
		storeID, err := fetchStoreID(client, u)
		if err != nil {
			log.Printf("failed %s: %v", u, err)
			continue
		}
		time.Sleep(5 * time.Second) // be nice to their servers
		fmt.Printf("%s\t%s\n", storeID, u)
	}
}

func fetchSitemap(client *http.Client, url string) ([]string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get sitemap: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get sitemap: status %s", resp.Status)
	}

	var s urlset
	if err := xml.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("decode sitemap: %w", err)
	}

	out := make([]string, 0, len(s.URLs))
	for _, u := range s.URLs {
		if strings.TrimSpace(u.Loc) != "" {
			out = append(out, strings.TrimSpace(u.Loc))
		}
	}
	return out, nil
}

func fetchStoreID(client *http.Client, url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get page: status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read page: %w", err)
	}

	m := storeIDRe.FindSubmatch(body)
	if len(m) < 2 {
		return "", fmt.Errorf("store-id not found")
	}
	return string(m[1]), nil
}
