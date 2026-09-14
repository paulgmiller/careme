package recipes

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"

	"careme/internal/httpx"
)

// writeHTMLResponse commits HTML only after the complete view renders successfully.
func writeHTMLResponse(w http.ResponseWriter, render func(io.Writer) error) {
	var body bytes.Buffer
	if err := render(&body); err != nil {
		http.Error(w, "render HTML: "+err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.SetHTMLContentType(w)
	if _, err := body.WriteTo(w); err != nil {
		// The response is already committed; a second HTTP response cannot repair it.
		slog.Error("write HTML response", "error", err)
	}
}
