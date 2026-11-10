package mnfoodclub

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"careme/internal/ai"

	"github.com/samber/lo"
)

// catalogProduct contains the product fields used for ingredient sourcing.
// https://docs.bigcommerce.com/developer/api-reference/rest/admin/catalog/products/get-products
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
//
// https://docs.bigcommerce.com/developer/api-reference/rest/admin/catalog/product-variants/get-product-variants
type catalogVariant struct {
	ID                 int      `json:"id"`
	Price              *float32 `json:"price"`
	CalculatedPrice    float32  `json:"calculated_price"`
	InventoryLevel     int      `json:"inventory_level"`
	PurchasingDisabled bool     `json:"purchasing_disabled"`
}

// catalogCategory maps category IDs to their display names.
// https://docs.bigcommerce.com/developer/api-reference/rest/admin/catalog/categories/get-categories
type catalogCategory struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// catalogBrand maps brand IDs to their display names.
// https://docs.bigcommerce.com/developer/api-reference/rest/admin/catalog/brands/get-brands
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
// the purchasable variant with the highest inventory and use its regular and selling
// prices together. Inventory ties keep the first eligible variant in catalog order.
// The selected variant ID is retained separately for the cart-transfer API.
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
		if selected == nil || variant.InventoryLevel > selected.InventoryLevel {
			selected = &variant
		}
	}
	if selected == nil {
		return ai.InputIngredient{}, false, nil
	}
	if selected.ID <= 0 {
		return ai.InputIngredient{}, false, fmt.Errorf("missing selected variant ID")
	}
	regularPrice := product.Price
	if selected.Price != nil {
		regularPrice = *selected.Price
	}
	if !validPrice(regularPrice) || !validPrice(selected.CalculatedPrice) {
		return ai.InputIngredient{}, false, fmt.Errorf("invalid catalog price")
	}
	// A variant's calculated price is the selling price, including inherited sales.
	// Do not compare it with another variant's base price or the retail/MSRP field.
	// Raising regularPrice here is defensive: we have observed calculated prices above
	// the parent product's price, but not confirmed above the variant's own price.
	if selected.CalculatedPrice > regularPrice {
		regularPrice = selected.CalculatedPrice
	}
	ingredient := ai.InputIngredient{
		ProductID: strconv.Itoa(product.ID), VariantID: strconv.Itoa(selected.ID), Description: product.Name,
		Size: catalogSizeFromDescription(product.Name), PriceRegular: &regularPrice,
	}
	// PriceSale represents a discount for PercentOff(), rather than every selling
	// price. Equal prices need no sale value; a higher value would give negative savings.
	if selected.CalculatedPrice < regularPrice {
		ingredient.PriceSale = &selected.CalculatedPrice
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
