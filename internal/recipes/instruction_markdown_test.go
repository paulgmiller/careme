package recipes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderRecipeInstructionsPlacesListWithinProse(t *testing.T) {
	rendered, err := renderRecipeInstructions([]string{
		"Mix together:\n\n- 1 tablespoon olive oil\n- 2 garlic cloves, grated\n\nto make the garlic oil.",
	})
	require.NoError(t, err)
	require.Len(t, rendered, 1)

	html := string(rendered[0])
	assert.Contains(t, html, "<p>Mix together:</p>")
	assert.Contains(t, html, "<ul>")
	assert.Contains(t, html, "<li>1 tablespoon olive oil</li>")
	assert.Contains(t, html, "<li>2 garlic cloves, grated</li>")
	assert.Contains(t, html, "<p>to make the garlic oil.</p>")
}

func TestRenderRecipeInstructionsUsesSafeGoldmarkDefaults(t *testing.T) {
	rendered, err := renderRecipeInstructions([]string{
		"Mix <script>alert('no')</script> [oil](javascript:alert('no')).",
	})
	require.NoError(t, err)
	require.Len(t, rendered, 1)

	html := string(rendered[0])
	assert.NotContains(t, html, "<script>")
	assert.NotContains(t, strings.ToLower(html), "href=\"javascript:")
}
