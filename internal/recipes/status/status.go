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
	fmt.Fprintf(&b, "Considering %d out of %d ingredients\n", len(ings), originalCount)
	for _, sale := range Sales(ings) {
		b.WriteString(sale)
		b.WriteString("\n")
	}

	return b.String()
}

func Error(err error) string {
	if err == nil {
		return ""
	}
	return "Something went wrong: " + err.Error()
}

func StaplesRetrying() string {
	return "We're having some difficulty checking your store's ingredients. Still trying…"
}

func StaplesUnavailable() string {
	return "We're having some difficulty checking your store's ingredients right now. Please try again in a few minutes."
}

func Regen(instructions string, dismissed []ai.Recipe) string {
	var sb strings.Builder
	if len(instructions) > 0 {
		fmt.Fprintf(&sb, "Noodling on %q\n", instructions)
	}
	for _, d := range dismissed {
		fmt.Fprintf(&sb, "Tossing %s\n", d.Title)
	}
	if sb.Len() == 0 {
		return "Guess I'll keep going\n"
	}
	return sb.String()
}
