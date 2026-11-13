package fullcircle

import (
	"strings"

	"careme/internal/ai"
)

// produceBoxIngredients is the temporary box supplied by the user, not live
// account inventory. Contents are for the November 17, 2026 delivery.
// Replace this list when the box contents change.
func produceBoxIngredients() []ai.InputIngredient {
	items := []struct{ name, size string }{
		{"Crimini Mushroom", "0.5 lb"},
		{"Maitake Mushroom", "1 package (4 oz)"},
		{"Chef's Sampler Mushroom", "1 package (8 oz)"},
		{"King Richard Leek", "1 bunch"},
		{"Dill, Small", "1 package"},
		{"Rainbow Chard", "1 bunch"},
		{"Gold Topped Beets", "1 lb"},
		{"Baby Broccoli", "1 bunch"},
		{"Spaghetti Winter Squash", "1"},
		{"Ambrosia Apple", "2"},
		{"Celery", "1"},
		{"Flat Leaf Parsley", "1 bunch"},
		{"Green Kale", "1 bunch"},
		{"Kiwi", "2"},
		{"Red Grapefruit", "1"},
		{"Red Onion", "1"},
		{"Red Potatoes", "1 lb"},
		{"Red Topped Beets", "1 lb"},
	}
	ingredients := make([]ai.InputIngredient, 0, len(items))
	for _, item := range items {
		ingredients = append(ingredients, ai.InputIngredient{
			ProductID:   "fullcircle_produce_box_" + strings.ToLower(strings.ReplaceAll(item.name, " ", "_")),
			Description: "Produce box " + item.name,
			Size:        item.size,
			Brand:       "Full Circle",
			Categories:  []string{"Produce", "Produce box"},
			Grade:       &ai.IngredientGrade{Score: 10, Reason: "Fresh produce from the user-supplied Full Circle box."},
		})
	}
	return ingredients
}
