package kroger

// this is a subset of ProductSearchResponse200Data combining item and product we think will be useful
// TODO merge with ai.Ingredient
type Ingredient struct {
	ProductId   *string `json:"id,omitempty"`
	AisleNumber *string `json:"number,omitempty"`
	Brand       *string `json:"brand,omitempty"`
	// CountryOrigin       *string   `json:"countryOrigin,omitempty"`
	Description *string `json:"description,omitempty"`
	// Favorite    *bool   `json:"favorite,omitempty"` //what does this mean?
	// InventoryStockLevel *string   `json:"stockLevel,omitempty"`
	PriceSale    *float32 `json:"salePrice,omitempty"`
	PriceRegular *float32 `json:"regularPrice,omitempty"`
	Size         *string  `json:"size,omitempty"`
	// not used by llm.
	Categories *[]string `json:"categories,omitempty"`
	// Figure out what is in taxonomies
}

func toStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
