package recipes

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"careme/internal/ai"
)

type shoppingListGroup struct {
	Aisle string
	Items []*ai.Ingredient
}

func shoppingListForDisplay(ingredients []ai.Ingredient) []shoppingListGroup {
	items := make(map[string]*ai.Ingredient)
	var combined []*ai.Ingredient // maintain original ordering after deduping

	for _, ingredient := range ingredients {
		name := normalizeShoppingListName(ingredient.Name)
		if name == "" {
			continue
		}
		existing, ok := items[name]
		if !ok {
			item := &ai.Ingredient{
				ProductID:   strings.TrimSpace(ingredient.ProductID),
				AisleNumber: strings.TrimSpace(ingredient.AisleNumber),
				Name:        ingredient.Name, // show non normalized
				Quantity:    strings.TrimSpace(ingredient.Quantity),
				Price:       strings.TrimSpace(ingredient.Price),
			}
			items[name] = item
			combined = append(combined, item)

			continue
		}
		existing.Quantity = mergeShoppingQuantities(existing.Quantity, ingredient.Quantity)
	}

	slices.SortStableFunc(combined, func(a, b *ai.Ingredient) int {
		return compareShoppingAisles(strings.TrimSpace(a.AisleNumber), strings.TrimSpace(b.AisleNumber))
	})

	var groups []shoppingListGroup
	for _, item := range combined {
		aisle := strings.TrimSpace(item.AisleNumber)
		if len(groups) == 0 || groups[len(groups)-1].Aisle != shoppingAisleHeading(aisle) {
			groups = append(groups, shoppingListGroup{
				Aisle: shoppingAisleHeading(aisle),
			})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, item)
	}
	return groups
}

var shoppingQtyWithSuffixPattern = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s+(.+?)\s*$`)

func mergeShoppingQuantities(existing string, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	switch {
	case incoming == "":
		return existing
	case existing == "":
		return incoming
	}

	existingNumber, existingSuffix, okExisting := parseShoppingQuantity(existing)
	incomingNumber, incomingSuffix, okIncoming := parseShoppingQuantity(incoming)
	if okExisting && okIncoming && normalizeShoppingQuantitySuffix(existingSuffix) == normalizeShoppingQuantitySuffix(incomingSuffix) {
		return formatShoppingQuantity(existingNumber+incomingNumber, existingSuffix)
	}
	return existing + ", " + incoming
}

func parseShoppingQuantity(raw string) (float64, string, bool) {
	match := shoppingQtyWithSuffixPattern.FindStringSubmatch(beforeFirstComma(raw))
	if len(match) != 3 {
		return 0, "", false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, "", false
	}
	suffix := strings.TrimSpace(match[2])
	if suffix == "" {
		return 0, "", false
	}
	return value, suffix, true
}

func beforeFirstComma(value string) string {
	if before, _, ok := strings.Cut(value, ","); ok {
		return strings.TrimSpace(before)
	}
	return strings.TrimSpace(value)
}

func normalizeShoppingQuantitySuffix(suffix string) string {
	return strings.ToLower(strings.Join(strings.Fields(suffix), " "))
}

func formatShoppingQuantity(value float64, suffix string) string {
	if math.Abs(value-math.Round(value)) < 1e-9 {
		return fmt.Sprintf("%d %s", int64(math.Round(value)), suffix)
	}
	return fmt.Sprintf("%s %s", strconv.FormatFloat(value, 'f', -1, 64), suffix)
}

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

// should only be used for internal matching otherwise violate some kroger must show names unaltered agreement
func normalizeShoppingListName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, name)
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
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
