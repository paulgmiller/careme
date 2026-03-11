package albertsons

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Chain struct {
	Brand       string
	DisplayName string
	Domain      string
	PathPrefix  string
	IDPrefix    string
}

func (c Chain) SitemapURL() string {
	return "https://" + c.Domain + "/sitemap.xml"
}

var defaultChains = []Chain{
	{
		Brand:       "albertsons",
		DisplayName: "Albertsons",
		Domain:      "local.albertsons.com",
		IDPrefix:    "albertsons_",
	},
	{
		Brand:       "starmarket",
		DisplayName: "Star Market",
		Domain:      "local.starmarket.com",
		IDPrefix:    "starmarket_",
	},
	{
		Brand:       "safeway",
		DisplayName: "Safeway",
		Domain:      "local.safeway.com",
		PathPrefix:  "safeway",
		IDPrefix:    "safeway_",
	},
	{
		Brand:       "haggen",
		DisplayName: "Haggen",
		Domain:      "local.haggen.com",
		IDPrefix:    "haggen_",
	},
	{
		Brand:       "acmemarkets",
		DisplayName: "ACME Markets",
		Domain:      "local.acmemarkets.com",
		IDPrefix:    "acmemarkets_",
	},
}

type sitemapURLSet struct {
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

type StorePage struct {
	Chain       Chain
	URL         string
	State       string
	City        string
	AddressSlug string
}

type StoreSummary struct {
	ID      string   `json:"id"`
	Brand   string   `json:"brand"`
	Domain  string   `json:"domain"`
	StoreID string   `json:"store_id"`
	Name    string   `json:"name"`
	Address string   `json:"address"`
	City    string   `json:"city"`
	State   string   `json:"state"`
	ZipCode string   `json:"zip_code"`
	Phone   string   `json:"phone,omitempty"`
	URL     string   `json:"url"`
	Lat     *float64 `json:"lat,omitempty"`
	Lon     *float64 `json:"lon,omitempty"`
}

type yextProfile struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	MainPhone        string           `json:"mainPhone"`
	Address          yextAddress      `json:"address"`
	AppleActionLinks []yextActionLink `json:"appleActionLinks"`
}

type yextAddress struct {
	City       string `json:"city"`
	Line1      string `json:"line1"`
	PostalCode string `json:"postalCode"`
	Region     string `json:"region"`
}

type yextActionLink struct {
	QuickLinkURL string `json:"quickLinkUrl"`
}

var (
	yextProfilePrefix = []byte(`Yext.Profile = `)
	yextProfileSuffix = []byte(`; return Yext;`)
	geoPositionRe     = regexp.MustCompile(`meta name="geo\.position" content="([0-9.-]+);([0-9.-]+)"`)
)

func DefaultChains() []Chain {
	return slices.Clone(defaultChains)
}

func IsID(locationID string) bool {
	locationID = strings.TrimSpace(locationID)
	for _, chain := range defaultChains {
		storeID := strings.TrimPrefix(locationID, chain.IDPrefix)
		if storeID != "" && storeID != locationID {
			return true
		}
	}
	return false
}

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
		loc := strings.TrimSpace(item.Loc)
		if loc == "" {
			continue
		}
		urls = append(urls, loc)
	}
	return urls, nil
}

func FilterStorePages(urls []string, chains []Chain) []StorePage {
	pages := make([]StorePage, 0, len(urls))
	seen := make(map[string]struct{}, len(urls))
	for _, rawURL := range urls {
		page, ok := ParseStorePageURL(rawURL, chains)
		if !ok {
			continue
		}
		if _, exists := seen[page.URL]; exists {
			continue
		}
		seen[page.URL] = struct{}{}
		pages = append(pages, page)
	}
	return pages
}

func ParseStorePageURL(rawURL string, chains []Chain) (StorePage, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return StorePage{}, false
	}

	chain, ok := chainForHost(u.Host, chains)
	if !ok {
		return StorePage{}, false
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) == 1 && segments[0] == "" {
		return StorePage{}, false
	}

	offset := 0
	if chain.PathPrefix != "" {
		if len(segments) != 4 || !strings.EqualFold(segments[0], chain.PathPrefix) {
			return StorePage{}, false
		}
		offset = 1
	} else if len(segments) != 3 {
		return StorePage{}, false
	}

	address := strings.TrimSuffix(segments[offset+2], ".html")
	if address == segments[offset+2] || address == "" {
		return StorePage{}, false
	}

	state := strings.TrimSpace(segments[offset])
	city := strings.TrimSpace(segments[offset+1])
	if state == "" || city == "" {
		return StorePage{}, false
	}

	return StorePage{
		Chain:       chain,
		URL:         rawURL,
		State:       strings.ToUpper(state),
		City:        city,
		AddressSlug: address,
	}, true
}

func FetchStoreSummary(ctx context.Context, client *http.Client, pageURL string, chains []Chain) (*StoreSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build store page request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get store page: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get store page: status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read store page: %w", err)
	}

	return ExtractStoreSummary(pageURL, body, chains)
}

func ExtractStoreSummary(pageURL string, body []byte, chains []Chain) (*StoreSummary, error) {
	page, ok := ParseStorePageURL(pageURL, chains)
	if !ok {
		return nil, fmt.Errorf("store page URL %q is invalid", pageURL)
	}

	profileJSON, err := extractProfileJSON(body)
	if err != nil {
		return nil, err
	}

	var profile yextProfile
	if err := json.Unmarshal(profileJSON, &profile); err != nil {
		return nil, fmt.Errorf("decode yext profile: %w", err)
	}

	storeID := strings.TrimSpace(profile.ID)
	if storeID == "" {
		storeID, err = storeIDFromActionLinks(profile.AppleActionLinks)
		if err != nil {
			return nil, err
		}
	}

	address := strings.TrimSpace(profile.Address.Line1)
	city := strings.TrimSpace(profile.Address.City)
	state := strings.ToUpper(strings.TrimSpace(profile.Address.Region))
	zipCode := strings.TrimSpace(profile.Address.PostalCode)

	if address == "" {
		address = page.AddressSlug
	}
	if city == "" {
		city = page.City
	}
	if state == "" {
		state = page.State
	}

	name := strings.TrimSpace(profile.Name)
	if name == "" || strings.EqualFold(name, page.Chain.DisplayName) {
		switch {
		case address != "":
			name = page.Chain.DisplayName + " " + address
		case city != "":
			name = page.Chain.DisplayName + " " + city
		default:
			name = page.Chain.DisplayName
		}
	}

	lat, lon := extractGeoPosition(body)

	return &StoreSummary{
		ID:      page.Chain.IDPrefix + storeID,
		Brand:   page.Chain.Brand,
		Domain:  page.Chain.Domain,
		StoreID: storeID,
		Name:    name,
		Address: address,
		City:    city,
		State:   state,
		ZipCode: zipCode,
		Phone:   strings.TrimSpace(profile.MainPhone),
		URL:     pageURL,
		Lat:     lat,
		Lon:     lon,
	}, nil
}

func chainForHost(host string, chains []Chain) (Chain, bool) {
	for _, chain := range chains {
		if strings.EqualFold(host, chain.Domain) {
			return chain, true
		}
	}
	return Chain{}, false
}

func extractProfileJSON(body []byte) ([]byte, error) {
	start := bytes.Index(body, yextProfilePrefix)
	if start < 0 {
		return nil, fmt.Errorf("yext profile not found")
	}
	start += len(yextProfilePrefix)

	end := bytes.Index(body[start:], yextProfileSuffix)
	if end < 0 {
		return nil, fmt.Errorf("yext profile terminator not found")
	}

	return body[start : start+end], nil
}

func storeIDFromActionLinks(links []yextActionLink) (string, error) {
	for _, link := range links {
		if strings.TrimSpace(link.QuickLinkURL) == "" {
			continue
		}

		parsed, err := url.Parse(link.QuickLinkURL)
		if err != nil {
			continue
		}

		storeID := strings.TrimSpace(parsed.Query().Get("storeId"))
		if storeID != "" {
			return storeID, nil
		}
	}
	return "", fmt.Errorf("store id not found in yext profile")
}

func extractGeoPosition(body []byte) (*float64, *float64) {
	matches := geoPositionRe.FindSubmatch(body)
	if len(matches) != 3 {
		return nil, nil
	}

	lat, err := strconv.ParseFloat(string(matches[1]), 64)
	if err != nil {
		return nil, nil
	}
	lon, err := strconv.ParseFloat(string(matches[2]), 64)
	if err != nil {
		return nil, nil
	}
	return &lat, &lon
}
