package recipes

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"careme/internal/ai"
)

func shoppingAisleHeading(aisle string) string {
	aisle = strings.TrimSpace(aisle)
	if aisle == "" {
		return "Other items"
	}
	if _, err := strconv.Atoi(aisle); err == nil {
		return "Aisle " + aisle
	}
	if label, ok := knownShoppingAisleLabels[aisle]; ok {
		return label
	}
	// Some providers give category slugs instead of display aisle names.
	// Convert values like "fresh-vegetables" into a readable heading.
	parts := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(aisle))
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

var knownShoppingAisleLabels = map[string]string{
	"dairy-eggs":  "Dairy & eggs",
	"fresh-herbs": "Fresh herbs",
}

func compareShoppingAisles(a, b string) int {
	if a == "" && b == "" {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	aint, aerr := strconv.Atoi(a)
	bint, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return cmp.Compare(aint, bint)
	}
	return cmp.Compare(a, b)
}

type shoppingPurchaseItem struct {
	ai.ShoppingItem
	Size  string
	Aisle string
}

type shoppingPurchaseGroup struct {
	Aisle string
	Items []shoppingPurchaseItem
}

// The model already merged these items; only attach catalog details and group them.
func shoppingPurchasesForDisplay(items []ai.ShoppingItem, catalog []ai.InputIngredient) []shoppingPurchaseGroup {
	products := make(map[string]ai.InputIngredient, len(catalog))
	for _, product := range catalog {
		products[product.ProductID] = product
	}
	sorted := make([]shoppingPurchaseItem, 0, len(items))
	for _, item := range items {
		row := shoppingPurchaseItem{ShoppingItem: item}
		if product, ok := products[item.ProductID]; ok {
			row.VariantID = product.VariantID
			row.Name = product.Description
			row.Size = product.Size
			row.Aisle = strings.TrimSpace(product.AisleNumber)
		}
		sorted = append(sorted, row)
	}
	slices.SortStableFunc(sorted, func(a, b shoppingPurchaseItem) int { return compareShoppingAisles(a.Aisle, b.Aisle) })
	groups := make([]shoppingPurchaseGroup, 0)
	for _, item := range sorted {
		heading := shoppingAisleHeading(item.Aisle)
		if len(groups) == 0 || groups[len(groups)-1].Aisle != heading {
			groups = append(groups, shoppingPurchaseGroup{Aisle: heading})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, item)
	}
	return groups
}

// Wine recommendations can arrive after finalization, so add them on each render.
func shoppingPurchasesWithWine(purchases []shoppingPurchaseGroup, recipes []ai.Recipe, recommendations map[string]*ai.WineSelection) []shoppingPurchaseGroup {
	if purchases == nil {
		return nil
	}
	group := shoppingPurchaseGroup{Aisle: "Wine"}
	for _, recipe := range recipes {
		selection := recommendations[recipe.ComputeHash()]
		if selection == nil || len(selection.Wines) == 0 {
			continue
		}
		wine := selection.Wines[0] // Use the same first pick as the recipe card.
		group.Items = append(group.Items, shoppingPurchaseItem{
			ProductID: wine.ProductID, VariantID: wine.VariantID, Name: wine.Name, Needed: wine.Quantity,
		})
	}
	if len(group.Items) == 0 {
		return purchases
	}
	return append(purchases, group)
}
