package instacart

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMeasurement(t *testing.T) {
	for _, tt := range []struct {
		input, unit string
		quantity    float64
	}{
		{"2 cups", "cup", 2},
		{"0.5 lb", "pound", .5},
		{"1/3 cup", "cup", 1.0 / 3},
		{"1 1/2 tbsp", "tablespoon", 1.5},
		{"4½ tbsp", "tablespoon", 4.5},
		{"½ tsp", "teaspoon", .5},
		{"1 ½ cups", "cup", 1.5},
		{"2", "each", 2},
		{"3 EACH", "each", 3},
		{"250 ml", "milliliter", 250},
		{"2 fl oz", "fl oz ounce", 2},
		{"2 fluid ounces", "fl oz ounce", 2},
		{".25 kilograms", "kilogram", .25},
		{"2 bunches", "bunch", 2},
		{"2 cans", "can", 2},
		{"1 head", "head", 1},
		{"3 packages", "package", 3},
		{"1 bottle", "", 0},
		{"2 cloves", "", 0},
		{"1-2 cups", "", 0},
		{"1 cup / 16 tbsp", "", 0},
		{"1 cup, divided", "", 0},
		{"about 2 cups", "", 0},
		{"1 cup chopped", "", 0},
		{"0 cups", "", 0},
		{"-1 cup", "", 0},
		{"1/0 cup", "", 0},
		{"", "", 0},
		{"NaN cup", "", 0},
	} {
		t.Run(tt.input, func(t *testing.T) {
			got := parseMeasurement(tt.input)
			if tt.unit == "" {
				assert.Nil(t, got)
				return
			}
			if assert.Len(t, got, 1) {
				assert.Equal(t, tt.unit, got[0].Unit)
				assert.InDelta(t, tt.quantity, got[0].Quantity, 1e-10)
			}
		})
	}
}
