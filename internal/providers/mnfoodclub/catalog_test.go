package mnfoodclub

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogStockAndPrices(t *testing.T) {
	for _, test := range []struct {
		name          string
		change        func(*catalogProduct)
		wantCount     int
		regular, sale float32
		wantErr       string
	}{
		{name: "untracked zero stock", wantCount: 1, regular: 10},
		{name: "hidden", change: func(p *catalogProduct) { p.IsVisible = false }},
		{name: "disabled", change: func(p *catalogProduct) { p.Availability = "disabled" }},
		{name: "preorder", change: func(p *catalogProduct) { p.Availability = "preorder" }},
		{name: "out of stock product", change: func(p *catalogProduct) { p.InventoryTracking = "product" }},
		{name: "in stock product", change: func(p *catalogProduct) { p.InventoryTracking = "product"; p.InventoryLevel = 1 }, wantCount: 1, regular: 10},
		{name: "product tracking ignores variant stock", change: func(p *catalogProduct) {
			p.InventoryTracking = "product"
			p.InventoryLevel = 1
			p.Variants[0].InventoryLevel = 0
		}, wantCount: 1, regular: 10},
		{name: "all variants sold out", change: func(p *catalogProduct) { p.InventoryTracking = "variant" }},
		{name: "all variants disabled", change: func(p *catalogProduct) { p.Variants[0].PurchasingDisabled = true }},
		{name: "inherited sale", change: func(p *catalogProduct) { p.Variants[0].CalculatedPrice = 8 }, wantCount: 1, regular: 10, sale: 8},
		{name: "free product", change: func(p *catalogProduct) { p.Price = 0; p.Variants[0].CalculatedPrice = 0 }, wantCount: 1},
		{name: "variant regular override", change: func(p *catalogProduct) { p.Variants[0].Price = new(float32(12)); p.Variants[0].CalculatedPrice = 12 }, wantCount: 1, regular: 12},
		{name: "highest inventory eligible variant", change: func(p *catalogProduct) {
			p.InventoryTracking = "variant"
			p.Variants = []catalogVariant{
				{ID: 1, Price: new(float32(5)), CalculatedPrice: 1, InventoryLevel: 0},
				{ID: 2, Price: new(float32(6)), CalculatedPrice: 2, InventoryLevel: 100, PurchasingDisabled: true},
				{ID: 456, Price: new(float32(20)), CalculatedPrice: 18, InventoryLevel: 20},
				{ID: 4, Price: new(float32(14)), CalculatedPrice: 12, InventoryLevel: 3},
			}
		}, wantCount: 1, regular: 20, sale: 18},
		{name: "inventory tie keeps first variant", change: func(p *catalogProduct) {
			p.Variants = []catalogVariant{
				{ID: 456, CalculatedPrice: 10, InventoryLevel: 3},
				{ID: 789, CalculatedPrice: 5, InventoryLevel: 3},
			}
		}, wantCount: 1, regular: 10},
		{name: "highest inventory without variant tracking", change: func(p *catalogProduct) {
			p.Variants = []catalogVariant{
				{ID: 789, CalculatedPrice: 5, InventoryLevel: 1},
				{ID: 456, CalculatedPrice: 10, InventoryLevel: 2},
			}
		}, wantCount: 1, regular: 10},
		{name: "missing variant ID", change: func(p *catalogProduct) { p.Variants[0].ID = 0 }, wantErr: "missing selected variant ID"},
		{name: "invalid variant ID", change: func(p *catalogProduct) { p.Variants[0].ID = -1 }, wantErr: "missing selected variant ID"},
		{name: "adjusted price exceeds base", change: func(p *catalogProduct) { p.Variants[0].CalculatedPrice = 12 }, wantCount: 1, regular: 12},
		{name: "missing name", change: func(p *catalogProduct) { p.Name = "" }, wantErr: "missing product ID or name"},
		{name: "missing ID", change: func(p *catalogProduct) { p.ID = 0 }, wantErr: "missing product ID or name"},
		{name: "missing variants", change: func(p *catalogProduct) { p.Variants = nil }, wantErr: "missing included variants"},
		{name: "negative price", change: func(p *catalogProduct) { p.Variants[0].CalculatedPrice = -1 }, wantErr: "invalid catalog price"},
		{name: "nonfinite price", change: func(p *catalogProduct) { p.Price = float32(math.Inf(1)) }, wantErr: "invalid catalog price"},
		{name: "missing brand", change: func(p *catalogProduct) { p.BrandID = 42 }, wantErr: "missing brand 42"},
		{name: "missing category", change: func(p *catalogProduct) { p.Categories = []int{42} }, wantErr: "missing category 42"},
	} {
		t.Run(test.name, func(t *testing.T) {
			product := catalogProduct{ID: 123, Name: "Chickpeas - 15oz", Price: 10, IsVisible: true, Availability: "available", InventoryTracking: "none", Variants: []catalogVariant{{ID: 456, CalculatedPrice: 10}}}
			if test.change != nil {
				test.change(&product)
			}
			got, err := mapCatalog([]catalogProduct{product}, nil, nil)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, test.wantCount)
			if test.wantCount == 0 {
				return
			}
			assert.Equal(t, "123", got[0].ProductID)
			assert.Equal(t, "456", got[0].VariantID)
			assert.Equal(t, "15oz", got[0].Size)
			assert.Equal(t, test.regular, *got[0].PriceRegular)
			if test.sale != 0 {
				require.NotNil(t, got[0].PriceSale)
				assert.Equal(t, test.sale, *got[0].PriceSale)
			} else {
				assert.Nil(t, got[0].PriceSale)
			}
			assert.Nil(t, got[0].Grade)
		})
	}
}

func TestCatalogCategoryMapping(t *testing.T) {
	categories := []catalogCategory{{ID: 1, Name: "Shop All"}, {ID: 3, Name: "Fruit"}, {ID: 2, Name: "Produce"}}
	product := catalogProduct{ID: 123, Name: "Apples", BrandID: 1, Price: 5, Categories: []int{3, 1, 2}, IsVisible: true, Availability: "available", Variants: []catalogVariant{{ID: 456, CalculatedPrice: 5}}}
	got, err := mapCatalog([]catalogProduct{product}, categories, []catalogBrand{{ID: 1, Name: "Local Farm"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"Fruit", "Shop All", "Produce"}, got[0].Categories)
	assert.Empty(t, got[0].AisleNumber)
	assert.Equal(t, "Local Farm", got[0].Brand)
	assert.Empty(t, got[0].Size)
}
