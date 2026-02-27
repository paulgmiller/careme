package walmart

import (
	"encoding/json"
	"fmt"
)

// CatalogProducts represents the paginated catalog product payload.
type CatalogProducts struct {
	Items        []CatalogProduct `json:"items"`
	TotalResults int              `json:"totalResults"`
	Start        int              `json:"start"`
	NumItems     int              `json:"numItems"`
	NextPage     string           `json:"nextPage"`
}

// CatalogProduct represents a Walmart catalog product entry.
type CatalogProduct struct {
	ItemID             int64   `json:"itemId"`
	ParentItemID       int64   `json:"parentItemId"`
	Name               string  `json:"name"`
	MSRP               float64 `json:"msrp"`
	SalePrice          float64 `json:"salePrice"`
	UPC                string  `json:"upc"`
	CategoryPath       string  `json:"categoryPath"`
	ShortDescription   string  `json:"shortDescription"`
	LongDescription    string  `json:"longDescription"`
	BrandName          string  `json:"brandName"`
	ThumbnailImage     string  `json:"thumbnailImage"`
	MediumImage        string  `json:"mediumImage"`
	LargeImage         string  `json:"largeImage"`
	ProductTrackingURL string  `json:"productTrackingUrl"`
	CategoryNode       string  `json:"categoryNode"`
	Stock              string  `json:"stock"`
	CustomerRating     string  `json:"customerRating"`
	NumReviews         int     `json:"numReviews"`
	ModelNumber        string  `json:"modelNumber"`
	SellerInfo         string  `json:"sellerInfo"`
	Size               string  `json:"size"`
	Color              string  `json:"color"`
	Marketplace        bool    `json:"marketplace"`
}

// ParseCatalogProducts unmarshals catalog payloads from wrapped or array shapes.
func ParseCatalogProducts(data []byte) (*CatalogProducts, error) {
	var catalog CatalogProducts
	if err := json.Unmarshal(data, &catalog); err == nil {
		return &catalog, nil
	}

	var items []CatalogProduct
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("unmarshal catalog payload: %w", err)
	}
	return &CatalogProducts{
		Items:        items,
		TotalResults: len(items),
		NumItems:     len(items),
	}, nil
}
