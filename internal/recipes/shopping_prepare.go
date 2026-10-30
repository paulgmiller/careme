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
func prepareShoppingPurchases(purchases []shoppingPurchaseGroup, recipes []ai.Recipe, recommendations map[string]*ai.WineSelection, wineCatalog map[string][]ai.InputIngredient) ([]shoppingPurchaseGroup, error) {
	if purchases == nil {
		return nil, nil
	}
	var catalog []ai.InputIngredient
	for _, recipe := range recipes {
		catalog = append(catalog, wineCatalog[recipe.ComputeHash()]...)
	}
	products, err := shoppingCatalogLookup(catalog)
	if err != nil {
		return nil, err
	}
	var wineItems []shoppingPurchaseItem
	for _, recipe := range recipes {
		selection := recommendations[recipe.ComputeHash()]
		if selection == nil || len(selection.Wines) == 0 {
			continue
		}
		wine := selection.Wines[0] // Use the same first pick as the recipe card.
		item := ai.ShoppingItem{ProductID: wine.ProductID, Name: wine.Name, Needed: wine.Quantity}

		itemProducts := products
		if catalog, exists := wineCatalog[recipe.ComputeHash()]; exists {
			matched := false
			for _, product := range catalog {
				if product.ProductID == wine.ProductID {
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("selected wine %q is missing from its saved catalog", wine.ProductID)
			}
		} else {
			// Legacy picks must not borrow ordering IDs from another recipe's snapshot.
			itemProducts = nil
		}
		wineItems = append(wineItems, prepareShoppingItem(item, itemProducts))
	}
	if len(wineItems) == 0 {
		return purchases, nil
	}
	return append(append([]shoppingPurchaseGroup(nil), purchases...), shoppingPurchaseGroup{Aisle: "Wine", Items: wineItems}), nil
}

// prepareShoppingItems attaches server-owned metadata after quantity merging.
func prepareShoppingItems(items []ai.ShoppingItem, products map[string]ai.InputIngredient) []shoppingPurchaseItem {
	prepared := make([]shoppingPurchaseItem, 0, len(items))
	for _, item := range items {
		prepared = append(prepared, prepareShoppingItem(item, products))
	}
	return prepared
}

func prepareShoppingItem(item ai.ShoppingItem, products map[string]ai.InputIngredient) shoppingPurchaseItem {
	row := shoppingPurchaseItem{ShoppingItem: item}
	if product, ok := products[item.ProductID]; ok {
		row.VariantID = product.VariantID
		// Use the catalog's exact product description instead of the AI's item name.
		row.Name = product.Description
		row.Size = product.Size
		row.Aisle = strings.TrimSpace(product.AisleNumber)
	}
	return row
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
