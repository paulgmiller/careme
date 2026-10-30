package recipes

import (
	"fmt"
	"strings"

	"careme/internal/ai"
)

type shoppingPurchaseItem struct {
	ai.ShoppingItem
	Size  string
	Aisle string
}

// prepareShoppingPurchases adds late wine picks to the immutable food snapshot.
func prepareShoppingPurchases(purchases []shoppingPurchaseGroup, recommendations map[string]*ai.WineSelection, wineCatalog []ai.InputIngredient) ([]shoppingPurchaseGroup, error) {
	products, err := shoppingCatalogLookup(wineCatalog)
	if err != nil {
		return nil, err
	}
	var wineItems []ai.ShoppingItem
	for _, selection := range recommendations {
		if len(selection.Wines) == 0 {
			continue
		}
		wine := selection.Wines[0] // Use the same first pick as the recipe card.
		wineItems = append(wineItems, ai.ShoppingItem{
			ProductID: wine.ProductID,
			Name:      wine.Name,
			Needed:    wine.Quantity,
		})
	}
	winepurchases := prepareShoppingItems(wineItems, products)

	if len(wineItems) == 0 {
		return purchases, nil
	}
	return append(purchases, shoppingPurchaseGroup{Aisle: "Wine", Items: winepurchases}), nil
}

// prepareShoppingItems attaches server-owned metadata after quantity merging.
func prepareShoppingItems(items []ai.ShoppingItem, products map[string]ai.InputIngredient) []shoppingPurchaseItem {
	prepared := make([]shoppingPurchaseItem, 0, len(items))
	for _, item := range items {
		row := shoppingPurchaseItem{ShoppingItem: item}
		if product, ok := products[item.ProductID]; ok {
			row.VariantID = product.VariantID
			// Use the catalog's exact product description instead of the AI's item name.
			row.Name = product.Description
			row.Size = product.Size
			row.Aisle = strings.TrimSpace(product.AisleNumber)
		}
	}
	return prepared
}

// A product-ID lookup must not silently replace a saved selection with another UPC.
func shoppingCatalogLookup(catalog []ai.InputIngredient) (map[string]ai.InputIngredient, error) {
	products := make(map[string]ai.InputIngredient, len(catalog))
	for _, product := range catalog {
		if previous, ok := products[product.ProductID]; ok && previous.VariantID != product.VariantID {
			return nil, fmt.Errorf("conflicting selected variants for shopping product %q", product.ProductID)
		}
		products[product.ProductID] = product
	}
	return products, nil
}

// MatchingShoppingCatalog keeps only products referenced by recipe ingredient IDs.
func MatchingShoppingCatalog(ingredients []ai.Ingredient, catalog []ai.InputIngredient) []ai.InputIngredient {
	ids := make(map[string]bool, len(ingredients))
	for _, ingredient := range ingredients {
		if ingredient.ProductID != "" {
			ids[ingredient.ProductID] = true
		}
	}
	matched := make([]ai.InputIngredient, 0)
	for _, product := range catalog {
		if ids[product.ProductID] {
			matched = append(matched, product)
		}
	}
	return matched
}
