package fullcircle

import (
	"strings"

	"careme/internal/ai"
)

// produceBoxIngredients is the temporary box supplied by the user, not live
// account inventory. Replace this list when the box contents change.
func produceBoxIngredients() []ai.InputIngredient {
	items := []struct{ name, size string }{
		{"Valencia Oranges", "2"},
		{"Murcott Mandarins", "1 lb"},
		{"Hass Avocado", "1"},
		{"Broccoli", "1 bunch"},
		{"Red Leaf Lettuce", "1 head"},
		{"Red Chard", "1 bunch"},
		{"Red Radishes", "1 bunch"},
		{"Fennel Bulb with Fronds", "1"},
		{"Yellow Potatoes", "1 lb"},
		{"Red Onion", "1"},
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
