package kroger

import (
	"encoding/csv"
	"fmt"
	"io"
)

// this is a subset of ProductSearchResponse200Data combining item and product we think will be useful
// TODO merge with ai.Ingredient
type Ingredient struct {
	ProductId   *string `json:"id,omitempty"`
	AisleNumber *string `json:"number,omitempty"`
	Brand       *string `json:"brand,omitempty"`
	//CountryOrigin       *string   `json:"countryOrigin,omitempty"`
	Description *string `json:"description,omitempty"`
	//Favorite    *bool   `json:"favorite,omitempty"` //what does this mean?
	//InventoryStockLevel *string   `json:"stockLevel,omitempty"`
	PriceSale    *float32 `json:"salePrice,omitempty"`
	PriceRegular *float32 `json:"regularPrice,omitempty"`
	Size         *string  `json:"size,omitempty"`
	//not used by llm.
	Categories *[]string `json:"categories,omitempty"`
	//Figure out what is in taxonomies
}

// this is what we'll actually pass to the llm
func ToTSV(ingredient []Ingredient, w io.Writer) error {
	csvw := csv.NewWriter(w)
	csvw.Comma = '\t'
	header := []string{"ProductId", "AisleNumber", "Brand", "Description", "Size", "PriceRegular", "PriceSale"}
	csvw.Write(header)
	for _, i := range ingredient {
		if i.PriceSale == nil {
			i.PriceSale = i.PriceRegular
		}
		row := []string{
			toStr(i.ProductId),
			toStr(i.AisleNumber),
			toStr(i.Brand),
			toStr(i.Description),
			toStr(i.Size),
			floatToStr(i.PriceRegular),
			floatToStr(i.PriceSale),
			//todo add a dicount?
		}
		if len(header) != len(row) {
			return fmt.Errorf("header and row length mismatch: %d vs %d", len(header), len(row))
		}
		if err := csvw.Write(row); err != nil {
			return err
		}
	}
	csvw.Flush()
	return csvw.Error()
}

// toStr returns the string value if non-nil, or "empty" otherwise.
func floatToStr(f *float32) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *f)
}

func toStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
