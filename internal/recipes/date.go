package recipes

import (
	"testing"
	"time"
)

var recipeDateLookup func(string, time.Time) time.Time

// RegisterRecipeDate installs the provider date policy during registry initialization.
// The input date is already the local store day, normalized to midnight.
func RegisterRecipeDate(lookup func(string, time.Time) time.Time) {
	recipeDateLookup = lookup
}

func recipeDate(locationID string, date time.Time) time.Time {
	if recipeDateLookup != nil {
		return recipeDateLookup(locationID, date)
	}
	if testing.Testing() {
		return date
	}
	panic("recipe date lookup is not registered")
}
