package conversions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBrowserEvent(t *testing.T) {
	for _, event := range []Event{SignupCompleted, RecipeGeneration, RecipeSave} {
		assert.True(t, IsBrowserEvent(event), "expected %q to be a browser conversion", event)
	}
	assert.False(t, IsBrowserEvent("unknown"))
}
