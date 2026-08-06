package ai

import (
	"strings"
	"testing"

	"careme/internal/culinary"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptBuildersAcceptSelectedCulinaryGuidance(t *testing.T) {
	t.Parallel()

	guidance, err := culinary.NewLibrary(culinary.Browning())
	require.NoError(t, err)

	recipePrompt := buildRecipeSystemMessage(guidance)
	critiquePrompt := buildRecipeCritiqueSystemInstruction(guidance)

	assert.Contains(t, recipePrompt, "## browning")
	assert.Contains(t, critiquePrompt, "## browning")
	assert.NotContains(t, recipePrompt, "## salt")
	assert.NotContains(t, critiquePrompt, "## acid-timing")
	assert.True(t, strings.HasSuffix(critiquePrompt, "Return JSON only."))
}
