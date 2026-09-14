package recipes

import (
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/templates"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecipeResponsesDiscardPartialTemplateOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		render func(http.ResponseWriter)
	}{
		{"page", func(w http.ResponseWriter) {
			writeHTMLResponse(w, func(out io.Writer) error { return renderRecipePage(out, recipePageView{}) })
		}},
		{"thread", func(w http.ResponseWriter) { writeRecipeThread(w, recipeThreadView{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := templates.Recipe
			t.Cleanup(func() { templates.Recipe = original })
			templates.Recipe = template.Must(template.New("recipe.html").Funcs(template.FuncMap{
				"fail": func() (string, error) { return "", errors.New("deliberate template failure") },
			}).Parse(`partial page{{fail}}{{define "recipe_thread"}}partial thread{{fail}}{{end}}`))
			response := httptest.NewRecorder()
			tc.render(response)
			assert.Equal(t, http.StatusInternalServerError, response.Code)
			assert.NotContains(t, response.Body.String(), "partial page")
			assert.NotContains(t, response.Body.String(), "partial thread")
			assert.Contains(t, response.Body.String(), "deliberate template failure")
		})
	}
}

func TestHTMLResponseCommitsCompleteHTML(t *testing.T) {
	response := httptest.NewRecorder()
	writeHTMLResponse(response, func(out io.Writer) error {
		_, err := io.WriteString(out, "<p>Dinner is ready</p>")
		return err
	})
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Header().Get("Content-Type"), "text/html")
	assert.Equal(t, "<p>Dinner is ready</p>", response.Body.String())
}
