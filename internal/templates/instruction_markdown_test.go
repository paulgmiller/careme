package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInstructionMarkdownHTMLPlacesListWithinProse(t *testing.T) {
	markdown := "Mix together:\n\n- 1 tablespoon olive oil\n- 2 garlic cloves, grated\n\nto make the garlic oil."

	assert.Equal(t,
		`<p>Mix together:</p><ul><li>1 tablespoon olive oil</li><li>2 garlic cloves, grated</li></ul><p>to make the garlic oil.</p>`,
		string(instructionMarkdownHTML(markdown)),
	)
}

func TestInstructionMarkdownHTMLEscapesModelText(t *testing.T) {
	markdown := "Mix <script>alert('no')</script>:\n- <b>oil</b>"

	html := string(instructionMarkdownHTML(markdown))
	assert.NotContains(t, html, "<script>")
	assert.NotContains(t, html, "<b>")
	assert.Contains(t, html, `&lt;script&gt;alert(&#39;no&#39;)&lt;/script&gt;`)
	assert.Contains(t, html, `&lt;b&gt;oil&lt;/b&gt;`)
}
