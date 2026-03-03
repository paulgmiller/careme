package kroger

import (
	aitypes "careme/internal/ai/types"
	"fmt"
)

// this is a subset of ProductSearchResponse200Data combining item and product we think will be useful
type Ingredient struct {
	ProductId   *string `json:"id,omitempty"`
	AisleNumber *string `json:"number,omitempty"`
	Brand       *string `json:"brand,omitempty"`
	//CountryOrigin       *string   `json:"countryOrigin,omitempty"`
	Description *string `json:"description,omitempty"`
	//Favorite    *bool   `json:"favorite,omitempty"` //what does this mean?
	//InventoryStockLevel *string   `json:"stockLevel,omitempty"`
	PriceSale    *float32  `json:"salePrice,omitempty"`
	PriceRegular *float32  `json:"regularPrice,omitempty"`
	Size         *string   `json:"size,omitempty"`
	Categories   *[]string `json:"categories,omitempty"`
	//Figure out what is in taxonomies
}

func (i Ingredient) ToAI() aitypes.Ingredient {
	if i.PriceSale == nil {
		i.PriceSale = i.PriceRegular
	}
	return aitypes.Ingredient{
		ProductId:    toStr(i.ProductId),
		AisleNumber:  toStr(i.AisleNumber),
		Brand:        toStr(i.Brand),
		Description:  toStr(i.Description),
		PriceSale:    toFloat32(i.PriceSale),
		PriceRegular: toFloat32(i.PriceRegular),
		Size:         toStr(i.Size),
	}
}

func toStr(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func toFloat32(ptr *float32) string {
	if ptr == nil {
		return "unknown"
	}
	return fmt.Sprintf("%.2f", *ptr)
}
