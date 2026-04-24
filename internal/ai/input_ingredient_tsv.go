package ai

import (
	"encoding/csv"
	"fmt"
	"io"
)

func InputIngredientsToTSV(ingredients []InputIngredient, w io.Writer) error {
	csvw := csv.NewWriter(w)
	csvw.Comma = '\t'
	header := []string{"ProductId", "AisleNumber", "Brand", "Description", "Size", "PriceRegular", "PriceSale"}
	if err := csvw.Write(header); err != nil {
		return err
	}
	for _, ingredient := range ingredients {
		priceSale := ingredient.PriceSale
		if priceSale == nil {
			priceSale = ingredient.PriceRegular
		}
		row := []string{
			ingredient.ProductID,
			ingredient.AisleNumber,
			ingredient.Brand,
			ingredient.Description,
			ingredient.Size,
			priceToString(ingredient.PriceRegular),
			priceToString(priceSale),
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
