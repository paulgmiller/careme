package walmart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// ProductLookupResults is the typed response for Walmart product lookup.
type ProductLookupResults struct {
	Items        []ProductLookupProduct `json:"items"`
	TotalResults int                    `json:"totalResults"`
	NumItems     int                    `json:"numItems"`
}

// ProductLookupProduct is a subset of item fields returned by Walmart item lookup.
type ProductLookupProduct struct {
	ItemID                    int64   `json:"itemId"`
	ParentItemID              int64   `json:"parentItemId"`
	Name                      string  `json:"name"`
	MSRP                      float64 `json:"msrp"`
	SalePrice                 float64 `json:"salePrice"`
	UPC                       string  `json:"upc"`
	CategoryPath              string  `json:"categoryPath"`
	ShortDescription          string  `json:"shortDescription"`
	LongDescription           string  `json:"longDescription"`
	BrandName                 string  `json:"brandName"`
	ThumbnailImage            string  `json:"thumbnailImage"`
	MediumImage               string  `json:"mediumImage"`
	LargeImage                string  `json:"largeImage"`
	ProductTrackingURL        string  `json:"productTrackingUrl"`
	CategoryNode              string  `json:"categoryNode"`
	Stock                     string  `json:"stock"`
	AvailableOnline           bool    `json:"availableOnline"`
	CustomerRating            string  `json:"customerRating"`
	NumReviews                int     `json:"numReviews"`
	ModelNumber               string  `json:"modelNumber"`
	SellerInfo                string  `json:"sellerInfo"`
	Size                      string  `json:"size"`
	Color                     string  `json:"color"`
	Marketplace               bool    `json:"marketplace"`
	StandardShipRate          float64 `json:"standardShipRate"`
	TwoThreeDayShippingRate   float64 `json:"twoThreeDayShippingRate"`
	FreeShippingOver35Dollars bool    `json:"freeShippingOver35Dollars"`
}

// ProductLookup queries Walmart items by item IDs scoped to a store.
// docs: https://walmart.io/docs/affiliates/v1/product-lookup
func (c *Client) ProductLookup(ctx context.Context, produceIDs []string, storeID string) (*ProductLookupResults, error) {
	return c.productLookupWithLocation(ctx, produceIDs, storeID, "")
}

// ProductLookupByZIP queries Walmart items by item IDs scoped to a ZIP code.
func (c *Client) ProductLookupByZIP(ctx context.Context, produceIDs []string, zip string) (*ProductLookupResults, error) {
	return c.productLookupWithLocation(ctx, produceIDs, "", zip)
}

func (c *Client) productLookupWithLocation(ctx context.Context, produceIDs []string, storeID, zip string) (*ProductLookupResults, error) {
	normalizedIDs := normalizeProduceIDs(produceIDs)
	if len(normalizedIDs) == 0 {
		return nil, errors.New("at least one produce ID is required")
	}

	storeID = strings.TrimSpace(storeID)
	zip = strings.TrimSpace(zip)

	params := url.Values{}
	params.Set("ids", strings.Join(normalizedIDs, ","))
	switch {
	case storeID != "":
		params.Set("storeId", storeID)
	case zip != "":
		params.Set("zip", zip)
	default:
		return nil, errors.New("store ID or zip code is required")
	}

	raw, err := c.productLookupWithParams(ctx, params)
	if err != nil {
		var statusErr *StatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusBadRequest && isResultsNotFound6001(statusErr.Body) {
			// Walmart may return 400/code=6001 ("results not found") for unresolved IDs.
			// Normalize this to an empty result set.
			return &ProductLookupResults{
				Items:        []ProductLookupProduct{},
				TotalResults: 0,
				NumItems:     0,
			}, nil
		}
		return nil, err
	}

	results, err := ParseProductLookupResults(raw)
	if err != nil {
		return nil, fmt.Errorf("parse product lookup response: %w", err)
	}

	return results, nil
}

// ProductLookupCatalogItem resolves a catalog row in a store, retrying alternate IDs when Walmart rejects one.
// Retry order: itemId -> parentItemId -> upc.
// If all candidates are rejected with 400, it returns an empty result instead of an error.
func (c *Client) ProductLookupCatalogItem(ctx context.Context, item CatalogProduct, storeID string) (*ProductLookupResults, error) {
	return c.productLookupCatalogItemWithLocation(ctx, item, storeID, "")
}

// ProductLookupCatalogItemByZIP resolves a catalog row for a ZIP code, retrying alternate IDs when Walmart rejects one.
func (c *Client) ProductLookupCatalogItemByZIP(ctx context.Context, item CatalogProduct, zip string) (*ProductLookupResults, error) {
	return c.productLookupCatalogItemWithLocation(ctx, item, "", zip)
}

func (c *Client) productLookupCatalogItemWithLocation(ctx context.Context, item CatalogProduct, storeID, zip string) (*ProductLookupResults, error) {
	type candidate struct {
		kind string
		id   string
	}

	candidates := make([]candidate, 0, 3)
	if item.ItemID > 0 {
		candidates = append(candidates, candidate{
			kind: "itemId",
			id:   fmt.Sprintf("%d", item.ItemID),
		})
	}
	if item.ParentItemID > 0 && item.ParentItemID != item.ItemID {
		candidates = append(candidates, candidate{
			kind: "parentItemId",
			id:   fmt.Sprintf("%d", item.ParentItemID),
		})
	}
	if upc := strings.TrimSpace(item.UPC); upc != "" {
		candidates = append(candidates, candidate{
			kind: "upc",
			id:   upc,
		})
	}

	if len(candidates) == 0 {
		return &ProductLookupResults{
			Items:        []ProductLookupProduct{},
			TotalResults: 0,
			NumItems:     0,
		}, nil
	}

	for _, cand := range candidates {
		results, err := c.productLookupWithLocation(ctx, []string{cand.id}, storeID, zip)
		if err == nil {
			if len(results.Items) == 0 {
				// Candidate resolved to no product; try alternate identifiers.
				continue
			}
			return results, nil
		}

		var statusErr *StatusError
		if errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusBadRequest {
			slog.WarnContext(ctx, "walmart product lookup candidate rejected",
				"catalogItemID", item.ItemID,
				"candidateType", cand.kind,
				"candidateID", cand.id,
				"status", statusErr.StatusCode,
				"responseBody", statusErr.Body,
			)
			continue
		}

		return nil, err
	}

	return &ProductLookupResults{
		Items:        []ProductLookupProduct{},
		TotalResults: 0,
		NumItems:     0,
	}, nil
}

func normalizeProduceIDs(ids []string) []string {
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		normalized = append(normalized, id)
	}
	return normalized
}

// ParseProductLookupResults unmarshals product lookup payloads from wrapped, array, or single-object shapes.
func ParseProductLookupResults(data []byte) (*ProductLookupResults, error) {
	var wrapped struct {
		Items        []ProductLookupProduct `json:"items"`
		Results      []ProductLookupProduct `json:"results"`
		TotalResults int                    `json:"totalResults"`
		NumItems     int                    `json:"numItems"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil {
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(data, &shape); err == nil {
			_, hasItems := shape["items"]
			_, hasResults := shape["results"]
			_, hasTotal := shape["totalResults"]
			_, hasNum := shape["numItems"]

			if hasItems || hasResults || hasTotal || hasNum {
				items := wrapped.Items
				if len(items) == 0 && len(wrapped.Results) > 0 {
					items = wrapped.Results
				}

				totalResults := wrapped.TotalResults
				if totalResults == 0 {
					totalResults = len(items)
				}
				numItems := wrapped.NumItems
				if numItems == 0 {
					numItems = len(items)
				}
				return &ProductLookupResults{
					Items:        items,
					TotalResults: totalResults,
					NumItems:     numItems,
				}, nil
			}
		}
	}

	var items []ProductLookupProduct
	if err := json.Unmarshal(data, &items); err == nil {
		return &ProductLookupResults{
			Items:        items,
			TotalResults: len(items),
			NumItems:     len(items),
		}, nil
	}

	var item ProductLookupProduct
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("unmarshal product lookup payload: %w", err)
	}

	return &ProductLookupResults{
		Items:        []ProductLookupProduct{item},
		TotalResults: 1,
		NumItems:     1,
	}, nil
}

func (c *Client) productLookupWithParams(ctx context.Context, params url.Values) (json.RawMessage, error) {
	lookupURL, err := url.Parse(c.baseURL + "/items")
	if err != nil {
		return nil, fmt.Errorf("parse product lookup URL: %w", err)
	}
	lookupURL.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build product lookup request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	if err := c.applyAuthHeaders(req); err != nil {
		return nil, fmt.Errorf("apply walmart auth headers: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request product lookup: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		return nil, fmt.Errorf("read product lookup response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		// Walmart sometimes returns 404 for item sets with no available results.
		// Normalize this to an empty typed payload for callers.
		return []byte(`{"items":[],"totalResults":0,"numItems":0}`), nil
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body := strings.TrimSpace(buf.String())
		if resp.StatusCode == http.StatusBadRequest && isResultsNotFound6001(body) {
			// Treat Walmart code 6001 "Results not found" as an empty lookup.
			return []byte(`{"items":[],"totalResults":0,"numItems":0}`), nil
		}
		slog.ErrorContext(ctx, "received Walmart product lookup response",
			"status", resp.StatusCode,
			"body", body,
		)
		return nil, &StatusError{
			Operation:  "product lookup",
			StatusCode: resp.StatusCode,
			Body:       body,
		}
	}

	return buf.Bytes(), nil
}

func isResultsNotFound6001(body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}

	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err == nil {
		if payload.Code == 6001 {
			return true
		}
		if strings.Contains(strings.ToLower(payload.Message), "results not found") {
			return true
		}
		for _, entry := range payload.Errors {
			if entry.Code == 6001 {
				return true
			}
			if strings.Contains(strings.ToLower(entry.Message), "results not found") {
				return true
			}
		}
	}

	lower := strings.ToLower(body)
	return strings.Contains(lower, "6001") && strings.Contains(lower, "results not found")
}
