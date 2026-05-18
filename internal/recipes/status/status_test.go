package status

import (
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
)

func TestSalesListsOnlyDiscountedIngredients(t *testing.T) {
	got := Sales([]ai.InputIngredient{
		{
			Description:  "Full Price Chicken",
			PriceRegular: new(float32(10)),
		},
		{
			Description:  "Half Off Spinach",
			PriceRegular: new(float32(10)),
			PriceSale:    new(float32(5)),
		},
		{
			Description:  "Same Price Pasta",
			PriceRegular: new(float32(10)),
			PriceSale:    new(float32(10)),
		},
		{
			Description:  "Twenty Off Salmon",
			PriceRegular: new(float32(10)),
			PriceSale:    new(float32(8)),
		},
	})

	assert.Equal(t, []string{
		"Half Off Spinach 50% off at 5.00",
		"Twenty Off Salmon 20% off at 8.00",
	}, got)
}

func TestIngredientsIncludesCountAndSales(t *testing.T) {
	got := Ingredients([]ai.InputIngredient{
		{
			Description:  "Half Off Spinach",
			PriceRegular: new(float32(10)),
			PriceSale:    new(float32(5)),
		},
	}, 3)

	assert.Equal(t, "Considering 1 out of 3 ingredients\nHalf Off Spinach 50% off at 5.00\n", got)
}
