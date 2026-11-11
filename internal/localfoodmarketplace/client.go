// Package localfoodmarketplace reads the public catalog used by LFM storefronts.
package localfoodmarketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	stdhtml "html"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/httpretry"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	catalogListType     = "ShowAll"
	catalogCustomListID = "0"
	catalogCategoryID   = ""
	catalogCallingPage  = "Products"
)

// Storefront supplies the host and provider-specific catalog defaults.
type Storefront struct {
	BaseURL     string
	SubPeriodID string
	LocationID  string
}

type client struct {
	storefront Storefront
	httpClient *http.Client
}

// Selection includes products matching any category or subcategory ID.
// An empty selection includes the full catalog.
type Selection struct {
	CategoryIDs    []int
	SubcategoryIDs []int
}

// catalogRequest is the JSON body posted by the storefront's getProducts()
// JavaScript. IDs remain strings to match the hidden HTML input values.
type catalogRequest struct {
	SubPeriodID  string `json:"subPeriodId"`  // Configured storefront selectedSubId value.
	ListType     string `json:"listType"`     // Catalog view, such as ShowAll.
	CustomListID string `json:"customListId"` // Storefront's selected custom list; normally "0".
	CategoryID   string `json:"catId"`        // Page's category scope; empty on /Products.
	LocationID   string `json:"locId"`        // Configured storefront currentLocId value, not a Careme location ID.
	CallingPage  string `json:"callingPage"`  // Fixed "Products" value supplied by the JavaScript.
}

func NewClient(storefront Storefront, httpClient *http.Client) *client {
	copyClient := *httpClient
	copyClient.Transport = otelhttp.NewTransport(copyClient.Transport)
	if copyClient.Timeout == 0 {
		copyClient.Timeout = 30 * time.Second
	}
	retry := retryablehttp.NewClient()
	retry.HTTPClient = &copyClient
	retry.Logger = nil
	retry.RequestLogHook = httpretry.LogRetry("localfoodmarketplace")
	storefront.BaseURL = strings.TrimRight(storefront.BaseURL, "/")
	return &client{storefront: storefront, httpClient: retry.StandardClient()}
}

// Fetch reads the public catalog directly using the configured storefront IDs.
// Category filtering is local, matching the storefront JavaScript.
func (c *client) Fetch(ctx context.Context, selection Selection) ([]ai.InputIngredient, error) {
	params := catalogRequest{
		SubPeriodID:  c.storefront.SubPeriodID,
		LocationID:   c.storefront.LocationID,
		ListType:     catalogListType,
		CustomListID: catalogCustomListID,
		CategoryID:   catalogCategoryID,
		CallingPage:  catalogCallingPage,
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encode catalog parameters: %w", err)
	}
	var catalog struct {
		Standard []product `json:"standard"`
		Featured []product `json:"featured"`
	}
	err = c.request(ctx, http.MethodPost, "/api/ProductApi/getProducts", body, func(r io.Reader) error {
		return json.NewDecoder(r).Decode(&catalog)
	})
	if err != nil {
		return nil, err
	}
	return ingredients(append(catalog.Standard, catalog.Featured...), selection)
}

func (c *client) request(ctx context.Context, method, path string, body []byte, read func(io.Reader) error) error {
	req, err := http.NewRequestWithContext(ctx, method, c.storefront.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create LFM request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch LFM %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch LFM %s: HTTP %d", path, resp.StatusCode)
	}
	if err := read(resp.Body); err != nil {
		return fmt.Errorf("parse LFM %s: %w", path, err)
	}
	return nil
}

type product struct {
	UniqueID      string `json:"uniqueId"`
	Name          string `json:"productName"`
	Producer      string `json:"producerName"`
	CategoryID    int    `json:"catId"`
	SubcategoryID int    `json:"subCatId"`
	Category      string `json:"parentCat"`
	Subcategory   string `json:"mainCat"`
	Unavailable   bool   `json:"unavailable"`
	HidePrices    bool   `json:"hidePrices"`
	Units         []struct {
		ID           int     `json:"puId"`
		Name         string  `json:"unitName"`
		Price        string  `json:"unitPrice"`
		RegularPrice string  `json:"unitRegPrice"`
		OnSale       bool    `json:"unitOnSale"`
		Remaining    float64 `json:"remaining"`
		PreOrder     bool    `json:"preOrder"`
	} `json:"units"`
}

func ingredients(products []product, selection Selection) ([]ai.InputIngredient, error) {
	allProducts := len(selection.CategoryIDs) == 0 && len(selection.SubcategoryIDs) == 0
	wanted := map[int]bool{}
	for _, category := range selection.CategoryIDs {
		wanted[category] = true
	}
	wantedSubcategories := map[int]bool{}
	for _, subcategory := range selection.SubcategoryIDs {
		wantedSubcategories[subcategory] = true
	}
	seen := map[string]bool{}
	found := map[int]bool{}
	foundSubcategories := map[int]bool{}
	var items []ai.InputIngredient
	for _, p := range products {
		if !allProducts && !wanted[p.CategoryID] && !wantedSubcategories[p.SubcategoryID] {
			continue
		}
		found[p.CategoryID] = true
		foundSubcategories[p.SubcategoryID] = true
		if p.Unavailable {
			continue
		}
		if strings.TrimSpace(p.UniqueID) == "" || strings.TrimSpace(p.Name) == "" || p.Category == "" {
			return nil, fmt.Errorf("catalog product missing identity, name, or category")
		}
		if seen[p.UniqueID] {
			continue
		}
		if len(p.Units) == 0 {
			return nil, fmt.Errorf("product %s has no units", p.UniqueID)
		}
		for _, unit := range p.Units {
			if unit.Remaining <= 0 && !unit.PreOrder {
				continue
			}
			if p.HidePrices || unit.ID <= 0 || strings.TrimSpace(unit.Name) == "" {
				return nil, fmt.Errorf("product %s missing unit or public price", p.UniqueID)
			}
			price, err := parsePrice(unit.Price)
			if err != nil {
				return nil, fmt.Errorf("product %s: %w", p.UniqueID, err)
			}
			item := ai.NormalizeInputIngredient(ai.InputIngredient{ProductID: p.UniqueID, VariantID: strconv.Itoa(unit.ID), Description: stdhtml.UnescapeString(p.Name), Brand: stdhtml.UnescapeString(p.Producer), Size: unit.Name, PriceRegular: &price, AisleNumber: p.Category, Categories: []string{p.Category}})
			if p.Subcategory != "" {
				item.Categories = append(item.Categories, p.Subcategory)
			}
			if unit.OnSale {
				regular, err := parsePrice(unit.RegularPrice)
				if err != nil {
					return nil, fmt.Errorf("product %s regular price: %w", p.UniqueID, err)
				}
				item.PriceRegular, item.PriceSale = &regular, &price
			}
			items = append(items, item)
			seen[p.UniqueID] = true
			break // One available purchase unit per product, matching recipe deduplication.
		}
	}
	for _, category := range selection.CategoryIDs {
		if !found[category] {
			return nil, fmt.Errorf("catalog missing category %d", category)
		}
	}
	for _, subcategory := range selection.SubcategoryIDs {
		if !foundSubcategories[subcategory] {
			return nil, fmt.Errorf("catalog missing subcategory %d", subcategory)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("catalog has no available ingredients")
	}
	return items, nil
}

func parsePrice(value string) (float32, error) {
	price, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(value), "$"), ",", ""), 32)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
		return 0, fmt.Errorf("invalid price %q", value)
	}
	return float32(price), nil
}
