package instacart

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Match the whole amount: trailing notes, ranges, and combined amounts require review.
var (
	amountPattern   = regexp.MustCompile(`^(\d+\s+\d+/\d+|\d+/\d+|\d*\.\d+|\d+)(?:\s*([a-z ]+))?$`)
	vulgarFractions = strings.NewReplacer("¼", " 1/4", "½", " 1/2", "¾", " 3/4", "⅐", " 1/7", "⅑", " 1/9", "⅒", " 1/10", "⅓", " 1/3", "⅔", " 2/3", "⅕", " 1/5", "⅖", " 2/5", "⅗", " 3/5", "⅘", " 4/5", "⅙", " 1/6", "⅚", " 5/6", "⅛", " 1/8", "⅜", " 3/8", "⅝", " 5/8", "⅞", " 7/8")
)

var unitAliases = map[string]string{
	"": "each", "each": "each",
	"cup": "cup", "cups": "cup", "c": "cup",
	"tbsp": "tablespoon", "tb": "tablespoon", "tbs": "tablespoon", "tablespoon": "tablespoon", "tablespoons": "tablespoon",
	"tsp": "teaspoon", "ts": "teaspoon", "tspn": "teaspoon", "teaspoon": "teaspoon", "teaspoons": "teaspoon",
	"fl oz": "fl oz ounce", "fluid ounce": "fl oz ounce", "fluid ounces": "fl oz ounce",
	"gallon": "gallon", "gallons": "gallon", "gal": "gallon", "gals": "gallon",
	"ml": "milliliter", "mls": "milliliter", "milliliter": "milliliter", "milliliters": "milliliter", "millilitre": "milliliter", "millilitres": "milliliter",
	"l": "liter", "liter": "liter", "liters": "liter", "litre": "liter", "litres": "liter",
	"pint": "pint", "pints": "pint", "pt": "pint", "pts": "pint",
	"quart": "quart", "quarts": "quart", "qt": "quart", "qts": "quart",
	"oz": "ounce", "ounce": "ounce", "ounces": "ounce",
	"lb": "pound", "lbs": "pound", "pound": "pound", "pounds": "pound",
	"g": "gram", "gs": "gram", "gram": "gram", "grams": "gram",
	"kg": "kilogram", "kgs": "kilogram", "kilogram": "kilogram", "kilograms": "kilogram",
	"bunch": "bunch", "bunches": "bunch", "can": "can", "cans": "can",
	"head": "head", "heads": "head", "package": "package", "packages": "package", "packet": "packet", "packets": "packet", "ear": "ears", "ears": "ears",
}

func parseMeasurement(raw string) []measurement {
	normalized := strings.Join(strings.Fields(vulgarFractions.Replace(strings.ToLower(raw))), " ")
	parts := amountPattern.FindStringSubmatch(normalized)
	if parts == nil {
		return nil
	}
	unit, ok := unitAliases[parts[2]]
	if !ok {
		return nil
	}
	var quantity float64
	for _, part := range strings.Fields(parts[1]) {
		numerator, denominator, fraction := strings.Cut(part, "/")
		value, err := strconv.ParseFloat(numerator, 64)
		if err != nil {
			return nil
		}
		if fraction {
			divisor, err := strconv.ParseFloat(denominator, 64)
			if err != nil || divisor == 0 {
				return nil
			}
			value /= divisor
		}
		quantity += value
	}
	if quantity <= 0 || math.IsInf(quantity, 0) || math.IsNaN(quantity) {
		return nil
	}
	return []measurement{{Quantity: quantity, Unit: unit}}
}
