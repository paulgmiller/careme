package culinary

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultLibraryKeepsGuidanceInIndependentModules(t *testing.T) {
	t.Parallel()

	library := DefaultLibrary()
	recipePrompt := library.RecipePrompt()
	critiquePrompt := library.CritiquePrompt()

	for _, id := range []string{"salt", "browning", "acid-timing"} {
		assert.Equal(t, 1, strings.Count(recipePrompt, "## "+id+"\n"))
		assert.Equal(t, 1, strings.Count(critiquePrompt, "## "+id+"\n"))
	}
	assert.Contains(t, recipePrompt, "Presalting meat")
	assert.Contains(t, recipePrompt, "remove excess surface moisture")
	assert.Contains(t, recipePrompt, "cook onions and other firm vegetables")
	assert.Contains(t, critiquePrompt, "2% salinity")
	assert.Contains(t, critiquePrompt, "avoid crowding")
	assert.Contains(t, critiquePrompt, "soften before acidic ingredients")
}

func TestLibraryCanRenderOnlySelectedModules(t *testing.T) {
	t.Parallel()

	library, err := NewLibrary(Browning())
	require.NoError(t, err)

	assert.Contains(t, library.RecipePrompt(), "## browning")
	assert.Contains(t, library.CritiquePrompt(), "## browning")
	assert.NotContains(t, library.RecipePrompt(), "## salt")
	assert.NotContains(t, library.CritiquePrompt(), "## acid-timing")
}

func TestNewLibraryRejectsDuplicateModuleIDs(t *testing.T) {
	t.Parallel()

	_, err := NewLibrary(Browning(), Browning())

	require.EqualError(t, err, `duplicate culinary guidance module "browning"`)
}

func TestNewLibraryRejectsEmptyRules(t *testing.T) {
	t.Parallel()

	_, err := NewLibrary(Module{ID: "empty", RecipeRules: []string{" "}})

	require.EqualError(t, err, `culinary guidance module "empty" has an empty recipe rule`)
}
