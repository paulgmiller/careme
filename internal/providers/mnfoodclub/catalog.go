package mnfoodclub

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"careme/internal/ai"

	"github.com/samber/lo"
)

type catalogProduct struct {
	ID                int              `json:"id"`
	Name              string           `json:"name"`
	BrandID           int              `json:"brand_id"`
	Categories        []int            `json:"categories"`
	Price             float32          `json:"price"`
	IsVisible         bool             `json:"is_visible"`
	Availability      string           `json:"availability"`
	InventoryTracking string           `json:"inventory_tracking"`
	InventoryLevel    int              `json:"inventory_level"`
	Variants          []catalogVariant `json:"variants"`
}

// catalogVariant is a purchasable option combination within a parent product,
// with its own price and, when tracked by variant, stock level. Products without
// options still have a base variant. MNFoodClub uses options for package sizes
// (e.g. 14oz vs 22oz lard), dietary choices (regular vs gluten-free pie crust),
// flavors (classic vs BBQ pork mac and cheese), and configurable bundles. A bundle
// can have hundreds of variants combining proteins, sides, dressings, and other
// choices; variants do not necessarily represent different sizes.
type catalogVariant struct {
	Price              *float32 `json:"price"`
	CalculatedPrice    float32  `json:"calculated_price"`
	InventoryLevel     int      `json:"inventory_level"`
	PurchasingDisabled bool     `json:"purchasing_disabled"`
}

type catalogCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type catalogBrand struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func mapCatalog(products []catalogProduct, categories []catalogCategory, brands []catalogBrand) ([]ai.InputIngredient, error) {
	categoryMap := lo.SliceToMap(categories, func(category catalogCategory) (int, catalogCategory) {
		return category.ID, category
	})
	brandMap := lo.SliceToMap(brands, func(brand catalogBrand) (int, string) {
		return brand.ID, brand.Name
	})
	var ingredients []ai.InputIngredient
	for _, product := range products {
		if !product.IsVisible || product.Availability != "available" || (product.InventoryTracking == "product" && product.InventoryLevel <= 0) {
			continue
		}
		ingredient, eligible, err := productIngredient(product, categoryMap, brandMap)
		if err != nil {
			return nil, fmt.Errorf("BigCommerce product %d: %w", product.ID, err)
		}
		if eligible {
			ingredients = append(ingredients, ingredient)
		}
	}
	return ingredients, nil
}

// productIngredient maps a product to one ingredient, keeping the parent product ID.
// BigCommerce includes a base variant even for products without options; products
// with options can have variants with different prices and stock levels. We choose
// the cheapest purchasable variant and use its regular and selling prices together.
// It returns eligible=false with no error when every variant is purchasing-disabled
// or, with variant-level inventory tracking, out of stock. Missing variant data is
// an error rather than an ineligible product.
func productIngredient(product catalogProduct, categories map[int]catalogCategory, brands map[int]string) (ai.InputIngredient, bool, error) {
	if product.ID <= 0 || strings.TrimSpace(product.Name) == "" {
		return ai.InputIngredient{}, false, fmt.Errorf("missing product ID or name")
	}
	if len(product.Variants) == 0 {
		return ai.InputIngredient{}, false, fmt.Errorf("missing included variants")
	}
	var selected *catalogVariant
	for _, variant := range product.Variants {
		if variant.PurchasingDisabled || (product.InventoryTracking == "variant" && variant.InventoryLevel <= 0) {
			continue
		}
		if selected == nil || variant.CalculatedPrice < selected.CalculatedPrice {
			selected = &variant
		}
	}
	if selected == nil {
		return ai.InputIngredient{}, false, nil
	}
	regular := product.Price
	if selected.Price != nil {
		regular = *selected.Price
	}
	current := selected.CalculatedPrice
	if !validPrice(regular) || !validPrice(current) {
		return ai.InputIngredient{}, false, fmt.Errorf("invalid catalog price")
	}
	// A variant's calculated price is the selling price, including inherited sales.
	// Do not compare it with another variant's base price or the retail/MSRP field.
	if current > regular {
		regular = current
	}
	ingredient := ai.InputIngredient{
		ProductID: strconv.Itoa(product.ID), Description: product.Name,
		Size: catalogSizeFromDescription(product.Name), PriceRegular: &regular,
	}
	if current < regular {
		ingredient.PriceSale = &current
	}
	if product.BrandID != 0 {
		brand, ok := brands[product.BrandID]
		if !ok {
			return ai.InputIngredient{}, false, fmt.Errorf("missing brand %d", product.BrandID)
		}
		ingredient.Brand = brand
	}
	// MNFoodClub is online-only, so leave AisleNumber unset. If shopping-list
	// grouping becomes useful, consider broad categories such as Meat or Produce
	// rather than the deepest catalog category (e.g. Thighs & Drumsticks).
	for _, id := range product.Categories {
		category, ok := categories[id]
		if !ok {
			return ai.InputIngredient{}, false, fmt.Errorf("missing category %d", id)
		}
		ingredient.Categories = append(ingredient.Categories, category.Name)
	}
	return ai.NormalizeInputIngredient(ingredient), true, nil
}

func validPrice(price float32) bool {
	return price >= 0 && !math.IsNaN(float64(price)) && !math.IsInf(float64(price), 0)
}
