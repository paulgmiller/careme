package status

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"careme/internal/ai"

	"github.com/samber/lo"
)

func Sales(ings []ai.InputIngredient) []string {
	sales := lo.Filter(ings, func(ing ai.InputIngredient, _ int) bool {
		return ing.PercentOff() > 0
	})
	slices.SortFunc(sales, func(a, b ai.InputIngredient) int {
		return cmp.Compare(b.PercentOff(), a.PercentOff()) // descending
	})

	return lo.Take(lo.Map(sales, func(ing ai.InputIngredient, _ int) string {
		return fmt.Sprintf("%s %.0f%% off at %.2f", ing.Description, ing.PercentOff(), *ing.PriceSale)
	}), 5)
}

func Ingredients(ings []ai.InputIngredient, originalCount int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Considering %d out of %d ingredients", len(ings), originalCount)
	for _, sale := range Sales(ings) {
		b.WriteString("\n")
		b.WriteString(sale)
	}
	return b.String()
}

func Titles(prefix string, recipes []ai.Recipe) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString("\n")
	for _, r := range recipes {
		b.WriteString(r.Title)
		b.WriteString("\n")
	}
	return b.String()
}

func Regen(instructions string, dismissed []ai.Recipe) string {
	var sb strings.Builder
	if len(instructions) > 0 {
		fmt.Fprintf(&sb, "Noodling on %q\n", instructions)
	}
	if len(dismissed) > 0 {
		sb.WriteString(Titles("Tossing out ", dismissed))
	}

	if sb.Len() == 0 {
		return "Guess I'll keep going"
	}
	return sb.String()
}
