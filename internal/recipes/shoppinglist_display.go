package recipes

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
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

type shoppingPurchaseGroup struct {
	Aisle string
	Items []shoppingPurchaseItem
}

// shoppingPurchasesForDisplay only sorts and groups prepared shopping items.
func shoppingPurchasesForDisplay(items []shoppingPurchaseItem) []shoppingPurchaseGroup {
	sorted := append([]shoppingPurchaseItem(nil), items...)
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
