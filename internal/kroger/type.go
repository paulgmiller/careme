package kroger

// this is a subset of ProductSearchResponse200Data combining item and product we think will be useful
// TODO merge with ai.Ingredient
type Ingredient struct {
	AisleNumber *string `json:"number,omitempty"`
	Brand       *string `json:"brand,omitempty"`
	//Categories          *[]string `json:"categories,omitempty"`
	CountryOrigin       *string   `json:"countryOrigin,omitempty"`
	Description         *string   `json:"description,omitempty"`
	Favorite            *bool     `json:"favorite,omitempty"` //what does this mean?
	InventoryStockLevel *string   `json:"stockLevel,omitempty"`
	PriceSale           *float32  `json:"salePrice,omitempty"`
	PriceRegular        *float32  `json:"regularPrice,omitempty"`
	Size                *string   `json:"size,omitempty"`
	Categories          *[]string `json:"categories,omitempty"`
}
