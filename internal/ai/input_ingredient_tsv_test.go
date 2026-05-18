package ai

import (
	"strings"
	"testing"
)

func TestInputIngredientsToTSV_UsesRegularPriceWhenSaleMissing(t *testing.T) {
	var buf strings.Builder
	err := InputIngredientsToTSV([]InputIngredient{{
		ProductID:    "item-1",
		AisleNumber:  "12",
		Brand:        "Acme",
		Description:  "Asparagus",
		Size:         "1 lb",
		PriceRegular: float32Ptr(4.99),
	}}, &buf)
	if err != nil {
		t.Fatalf("inputIngredientsToTSV returned error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "ProductId\tAisleNumber\tBrand\tDescription\tSize\tPriceRegular\tPriceSale") {
		t.Fatalf("expected TSV header, got %q", got)
	}
	if !strings.Contains(got, "item-1\t12\tAcme\tAsparagus\t1 lb\t4.99\t4.99") {
		t.Fatalf("expected regular price copied into sale column, got %q", got)
	}
}
