package mnfoodclub

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"careme/internal/ai"
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

type catalogVariant struct {
	Price              *float32 `json:"price"`
	CalculatedPrice    float32  `json:"calculated_price"`
	InventoryLevel     int      `json:"inventory_level"`
	PurchasingDisabled bool     `json:"purchasing_disabled"`
}

type catalogCategory struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	ParentID int    `json:"parent_id"`
}

type catalogBrand struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func mapCatalog(products []catalogProduct, categories []catalogCategory, brands []catalogBrand) ([]ai.InputIngredient, error) {
	categoryMap := make(map[int]catalogCategory, len(categories))
	for _, category := range categories {
		categoryMap[category.ID] = category
	}
	brandMap := make(map[int]string, len(brands))
	for _, brand := range brands {
		brandMap[brand.ID] = brand.Name
	}
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
	deepest := -1
	aisleID := 0
	for _, id := range product.Categories {
		category, ok := categories[id]
		if !ok {
			return ai.InputIngredient{}, false, fmt.Errorf("missing category %d", id)
		}
		ingredient.Categories = append(ingredient.Categories, category.Name)
		depth, err := categoryDepth(id, categories)
		if err != nil {
			return ai.InputIngredient{}, false, err
		}
		if depth > deepest || (depth == deepest && id < aisleID) {
			deepest, aisleID = depth, id
			ingredient.AisleNumber = category.Name
		}
	}
	return ai.NormalizeInputIngredient(ingredient), true, nil
}

func categoryDepth(id int, categories map[int]catalogCategory) (int, error) {
	seen := map[int]bool{}
	depth := 0
	for id != 0 {
		if seen[id] {
			return 0, fmt.Errorf("cycle in category ancestry at %d", id)
		}
		seen[id] = true
		category, ok := categories[id]
		if !ok {
			return 0, fmt.Errorf("missing category ancestor %d", id)
		}
		id = category.ParentID
		depth++
	}
	return depth, nil
}

func validPrice(price float32) bool {
	return price >= 0 && !math.IsNaN(float64(price)) && !math.IsInf(float64(price), 0)
}
