package albertsons

import (
	"bytes"
	"careme/internal/sitemapfetch"
	"context"
	"encoding/json"
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

/*
Acme Markets: 162 locations (CT, DE, MD, NJ, NY and PA)[158][159]
Albertsons: 381 locations (AZ, AR, CA, CO, ID, LA, MT, NV, NM, ND, OK, OR, TX, UT, WA and WY)[160]
Albertsons Market: 23 locations (NM)[161]
Amigos: 4 locations (TX)[162]
Andronico's: 7 locations (CA)[163]
Balducci's: 8 locations (CT, MD, NY, VA)[164]
Carrs: 11 locations (AK)[165]
Haggen: 15 locations (WA)[166]
Jewel-Osco: 188 locations (IL, IA, and IN)[167]
Kings Food Markets: 19 locations (CT, NJ, NY)[168]
Lucky: 4 locations (UT)[169]
Market Street: 19 locations (NM and TX)[170]
Pak 'n Save: 2 locations (CA)[171]
Pavilions: 27 locations (Southern California)[172]
Randalls: 28[173] locations (Greater Houston and Greater Austin, TX)[174]
Safeway: 914 locations (AK, AZ, CA, CO, DC, DE, HI, ID, MD, MT, NE, NV, NM, OR, SD, VA, WA, WY)[175]
Shaw's: 127 locations (MA, ME, NH, RI and VT)[176]
Star Market: 21 locations (MA)[177]
Tom Thumb: 65[173] locations (Dallas–Fort Worth metroplex, TX)[178]
United Supermarkets: 97 locations (Texas Panhandle) plus 39 United Express locations (NM and TX)[179]
Vons: 194 locations (Southern California and Southern Nevada)[180]
*/
var defaultChains = []Chain{
	{
		Brand:       "albertsons",
		DisplayName: "Albertsons",
		Domain:      "local.albertsons.com",
		IDPrefix:    "albertsons_",
	},
	{
		Brand:       "shaws",
		DisplayName: "Shaw's",
		Domain:      "local.shaws.com",
		IDPrefix:    "shaws_",
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
	{
		Brand:       "vons",
		DisplayName: "Vons",
		Domain:      "local.vons.com",
		IDPrefix:    "vons_",
	},
	{
		Brand:       "jewelosco",
		DisplayName: "Jewel-Osco",
		Domain:      "local.jewelosco.com",
		IDPrefix:    "jewelosco_",
	},
	{
		Brand:       "unitedsupermarkets",
		DisplayName: "United Supermarkets",
		Domain:      "local.unitedsupermarkets.com",
		IDPrefix:    "unitedsupermarkets_",
	},
	{
		Brand:       "tomthumb",
		DisplayName: "Tom Thumb",
		Domain:      "local.tomthumb.com",
		IDPrefix:    "tomthumb_",
	},
	{
		Brand:       "randalls",
		DisplayName: "Randalls",
		Domain:      "local.randalls.com",
		IDPrefix:    "randalls_",
	},
	{
		Brand:       "pavilions",
		DisplayName: "Pavilions",
		Domain:      "local.pavilions.com",
		IDPrefix:    "pavilions_",
	},
	{
		Brand:       "kingsfoodmarkets",
		DisplayName: "Kings Food Markets",
		Domain:      "local.kingsfoodmarkets.com",
		IDPrefix:    "kingsfoodmarkets_",
	},
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
	URL     string   `json:"url"`
	Lat     *float64 `json:"lat,omitempty"`
	Lon     *float64 `json:"lon,omitempty"`
}

type yextProfile struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Meta    yextMeta    `json:"meta"`
	Address yextAddress `json:"address"`
}

type yextAddress struct {
	City       string `json:"city"`
	Line1      string `json:"line1"`
	PostalCode string `json:"postalCode"`
	Region     string `json:"region"`
}

type yextMeta struct {
	ID string `json:"id"`
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
	return sitemapfetch.FetchURLs(ctx, client, sitemapURL)
}

func FilterStorePages(urls []string, chain Chain) []StorePage {
	pages := make([]StorePage, 0, len(urls))
	seen := make(map[string]struct{}, len(urls))
	for _, rawURL := range urls {
		page, ok := ParseStorePageURL(rawURL, chain)
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

// ParseStorePageURL accepts only store landing pages.
// Expected path shapes are:
//
//	/<state>/<city>/<address>.html
//	/<brand>/<state>/<city>/<address>.html
//
// The final segment must be the store address page; category/service pages like
// /produce.html or /bakery.html add an extra segment and are rejected.
func ParseStorePageURL(rawURL string, chain Chain) (StorePage, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return StorePage{}, false
	}

	if !strings.EqualFold(u.Host, chain.Domain) {
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

func FetchStoreSummary(ctx context.Context, client *http.Client, pageURL string, chain Chain) (*StoreSummary, error) {
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

	return ExtractStoreSummary(pageURL, body, chain)
}

func ExtractStoreSummary(pageURL string, body []byte, chain Chain) (*StoreSummary, error) {
	page, ok := ParseStorePageURL(pageURL, chain)
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
		storeID = strings.TrimSpace(profile.Meta.ID)
	}
	if storeID == "" {
		return nil, fmt.Errorf("store id not found in yext profile")
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

	// These pages often embed only the banner name ("Safeway", "Albertsons"),
	// so build a store-specific display name from the address when needed.
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

	//seems fragile?
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
		URL:     pageURL,
		Lat:     lat,
		Lon:     lon,
	}, nil
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
