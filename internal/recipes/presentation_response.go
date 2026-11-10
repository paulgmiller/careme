package recipes

import (
	"bytes"
	"html/template"
	"log/slog"
	"net/http"

	"careme/internal/httpx"
)

// renderHTML commits HTML only after the complete view renders successfully.
func renderHTML(w http.ResponseWriter, tmpl *template.Template, name string, view any) {
	var body bytes.Buffer
	if err := tmpl.ExecuteTemplate(&body, name, view); err != nil {
		http.Error(w, "render HTML: "+err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.SetHTMLContentType(w)
	if _, err := body.WriteTo(w); err != nil {
		// The response is already committed; a second HTTP response cannot repair it.
		slog.Error("write HTML response", "error", err)
	}
}
