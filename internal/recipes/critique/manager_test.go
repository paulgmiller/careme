package critique

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMinimumRecipeScoreForModel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 7, MinimumRecipeScoreForModel("anthropic/claude-opus-5"))
	assert.Equal(t, 7, MinimumRecipeScoreForModel("anthropic/claude-opus-5-20260724"))
	assert.Equal(t, 7, MinimumRecipeScoreForModel("anthropic/claude-opus-4.1"))
	assert.Equal(t, 8, MinimumRecipeScoreForModel("google/gemini-3.1-pro-preview"))
	assert.Equal(t, 8, MinimumRecipeScoreForModel(""))
}
