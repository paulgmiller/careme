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

func TestIngredientsListsSalesByGradeThenDiscount(t *testing.T) {
	ingredient := func(name string, score int, sale float32) ai.InputIngredient {
		return ai.InputIngredient{
			Description:  name,
			PriceRegular: new(float32(10)),
			PriceSale:    new(sale),
			Grade:        &ai.IngredientGrade{Score: score},
		}
	}
	ungraded := ingredient("Ungraded", 0, 1)
	ungraded.Grade = nil
	got := Ingredients([]ai.InputIngredient{
		ungraded,
		ingredient("Lower grade", 6, 2),
		ingredient("Top grade smaller discount", 10, 9),
		ingredient("Top grade larger discount", 10, 8),
		ingredient("Second grade", 9, 7),
		ingredient("Third grade", 8, 6),
	}, 6)
	assert.Equal(t, "Considering 6 out of 6 ingredients\n"+
		"Top grade larger discount 20% off at 8.00\n"+
		"Top grade smaller discount 10% off at 9.00\n"+
		"Second grade 30% off at 7.00\n"+
		"Third grade 40% off at 6.00\n"+
		"Lower grade 80% off at 2.00\n", got)
}
