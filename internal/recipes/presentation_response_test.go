package recipes

import (
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderHTMLDiscardsPartialTemplateOutput(t *testing.T) {
	t.Parallel()
	tmpl := template.Must(template.New("recipe.html").Funcs(template.FuncMap{
		"fail": func() (string, error) { return "", errors.New("deliberate template failure") },
	}).Parse(`partial page{{fail}}{{define "recipe_thread"}}partial thread{{fail}}{{end}}`))
	for _, name := range []string{"recipe.html", "recipe_thread", "missing_template"} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			renderHTML(response, tmpl, name, nil)
			assert.Equal(t, http.StatusInternalServerError, response.Code)
			assert.NotContains(t, response.Body.String(), "partial page")
			assert.NotContains(t, response.Body.String(), "partial thread")
			assert.Contains(t, response.Body.String(), "render HTML:")
		})
	}
}

func TestRenderHTMLCommitsCompleteHTML(t *testing.T) {
	t.Parallel()
	tmpl := template.Must(template.New("page").Parse(`<p>{{.}}</p>{{define "fragment"}}<span>{{.}}</span>{{end}}`))
	for _, tc := range []struct{ name, want string }{
		{"page", "<p>Dinner &amp; dessert</p>"},
		{"fragment", "<span>Dinner &amp; dessert</span>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			renderHTML(response, tmpl, tc.name, "Dinner & dessert")
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Contains(t, response.Header().Get("Content-Type"), "text/html")
			assert.Equal(t, tc.want, response.Body.String())
		})
	}
}
