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
		PriceRegular: new(float32(4.99)),
		PriceUnit:    "lb",
	}}, &buf)
	if err != nil {
		t.Fatalf("InputIngredientsToTSV returned error: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "ProductId\tBrand\tDescription\tSize\tPriceRegular\tPriceSale\tPriceUnit") {
		t.Fatalf("expected TSV header, got %q", got)
	}
	if strings.Contains(got, "AisleNumber") || strings.Contains(got, "\t12\t") {
		t.Fatalf("did not expect aisle information in prompt TSV, got %q", got)
	}
	if !strings.Contains(got, "item-1\tAcme\tAsparagus\t1 lb\t4.99\t4.99\tlb") {
		t.Fatalf("expected regular price copied into sale column, got %q", got)
	}
}
