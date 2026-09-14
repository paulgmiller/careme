package recipes

import (
	"testing"

	"careme/internal/ai"
	"github.com/stretchr/testify/assert"
)

func TestNewCookingMethodDisplay(t *testing.T) {
	tests := []struct {
		method ai.CookingMethod
		want   cookingMethodDisplay
	}{
		{method: ai.CookingMethodStovetop, want: cookingMethodDisplay{Label: "Stovetop", Emoji: "🍳"}},
		{method: ai.CookingMethodOven, want: cookingMethodDisplay{Label: "Oven", Emoji: "♨️"}},
		{method: ai.CookingMethodGrill, want: cookingMethodDisplay{Label: "Grill", Emoji: "🔥"}},
		{method: ai.CookingMethodSlowCooker, want: cookingMethodDisplay{Label: "Slow cooker", Emoji: "🍲"}},
		{method: ai.CookingMethodAirFryer, want: cookingMethodDisplay{Label: "Air fryer", Emoji: "🌀"}},
		{method: ai.CookingMethodNoCook, want: cookingMethodDisplay{Label: "No-cook", Emoji: "🥗"}},
		{method: ai.CookingMethodOther, want: cookingMethodDisplay{Label: "Other", Emoji: "❓"}},
		{method: ai.CookingMethod("microwave"), want: cookingMethodDisplay{}},
	}

	for _, tt := range tests {
		t.Run(string(tt.method), func(t *testing.T) {
			assert.Equal(t, tt.want, newCookingMethodDisplay(tt.method))
		})
	}
}
