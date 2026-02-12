package static

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
)

//go:embed tailwind.css
var tailwindCSS []byte

//go:embed htmx@2.0.8.js
var htmx208JS []byte

//go:embed favicon.png
var favicon []byte

var TailwindAssetPath string

func Init() {
	tailwindHash := fmt.Sprintf("%x", sha256.Sum256(tailwindCSS))
	TailwindAssetPath = fmt.Sprintf("/static/tailwind.%s.css", tailwindHash[:12])
}

// Register serves static assets and wires template asset paths.
func Register(mux *http.ServeMux) {

	mux.HandleFunc(TailwindAssetPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, err := w.Write(tailwindCSS); err != nil {
			slog.ErrorContext(r.Context(), "failed to write tailwind css", "error", err)
		}
	})

	// Intentionally versioned so that we can cache aggressively.
	mux.HandleFunc("/static/htmx@2.0.8.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, err := w.Write(htmx208JS); err != nil {
			slog.ErrorContext(r.Context(), "failed to write htmx js", "error", err)
		}
	})

	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, err := w.Write(favicon); err != nil {
			slog.ErrorContext(r.Context(), "failed to write favicon", "error", err)
		}
	})
}
