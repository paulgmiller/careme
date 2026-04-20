package static

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	texttemplate "text/template"

	"careme/internal/routing"
	"careme/internal/seasons"
)

//go:embed tailwind.css
var tailwindCSS []byte

//go:embed htmx@2.0.8.js
var htmx208JS []byte

//go:embed favicon-fall.png
var faviconFall []byte

//go:embed favicon-winter.png
var faviconWinter []byte

//go:embed favicon-spring.png
var faviconSpring []byte

//go:embed favicon-summer.png
var faviconSummer []byte

//go:embed app-icon-192.png
var appIcon192 []byte

//go:embed app-icon-512.png
var appIcon512 []byte

//go:embed manifest.webmanifest
var manifestWebmanifest []byte

//go:embed offline.html
var offlineHTML []byte

//go:embed sw.js.tmpl
var serviceWorkerJS []byte

var offlinePageTemplate = template.Must(template.New("offline").Parse(string(offlineHTML)))
var serviceWorkerTemplate = texttemplate.Must(texttemplate.New("sw").Parse(string(serviceWorkerJS)))

var TailwindAssetPath string

func Init() {
	tailwindHash := fmt.Sprintf("%x", sha256.Sum256(tailwindCSS))
	TailwindAssetPath = fmt.Sprintf("/static/tailwind.%s.css", tailwindHash[:12])
}

// Register serves static assets and wires template asset paths.
func Register(mux routing.Registrar) {
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
		// Keep cache short so clients can refresh seasonally without manual cache clear.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		favicon := faviconBySeason(seasons.GetCurrentSeason())
		if _, err := w.Write(favicon); err != nil {
			slog.ErrorContext(r.Context(), "failed to write favicon", "error", err)
		}
	})

	mux.HandleFunc("/static/app-icon-192.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, err := w.Write(appIcon192); err != nil {
			slog.ErrorContext(r.Context(), "failed to write 192 icon", "error", err)
		}
	})

	mux.HandleFunc("/static/app-icon-512.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, err := w.Write(appIcon512); err != nil {
			slog.ErrorContext(r.Context(), "failed to write 512 icon", "error", err)
		}
	})

	mux.HandleFunc("/manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if _, err := w.Write(manifestWebmanifest); err != nil {
			slog.ErrorContext(r.Context(), "failed to write web manifest", "error", err)
		}
	})

	mux.HandleFunc("/offline", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		page, err := renderOfflinePage()
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to render offline page", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(page); err != nil {
			slog.ErrorContext(r.Context(), "failed to write offline page", "error", err)
		}
	})

	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		script, err := renderServiceWorker()
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to render service worker", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
		if _, err := w.Write(script); err != nil {
			slog.ErrorContext(r.Context(), "failed to write service worker", "error", err)
		}
	})
}

func faviconBySeason(season seasons.Season) []byte {
	switch season {
	case seasons.Winter:
		return faviconWinter
	case seasons.Spring:
		return faviconSpring
	case seasons.Summer:
		return faviconSummer
	case seasons.Fall:
		fallthrough
	default:
		return faviconFall
	}
}

func renderOfflinePage() ([]byte, error) {
	scheme := seasons.GetCurrentColorScheme()
	data := struct {
		TailwindAssetPath string
		ThemeColor        string
		Colors            seasons.ColorScheme
	}{
		TailwindAssetPath: TailwindAssetPath,
		ThemeColor:        scheme.C600,
		Colors:            scheme,
	}

	var buf bytes.Buffer
	if err := offlinePageTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func renderServiceWorker() ([]byte, error) {
	precachePaths := []string{
		"/offline",
		"/manifest.webmanifest",
		"/favicon.ico",
		"/static/app-icon-192.png",
		"/static/app-icon-512.png",
		"/static/htmx@2.0.8.js",
		TailwindAssetPath,
	}
	authPaths := []string{"/sign-in", "/sign-up", "/auth/establish", "/logout"}

	precacheJSON, err := json.Marshal(precachePaths)
	if err != nil {
		return nil, err
	}
	authJSON, err := json.Marshal(authPaths)
	if err != nil {
		return nil, err
	}

	data := struct {
		CacheName    string
		PrecacheURLs string
		AuthPaths    string
	}{
		CacheName:    `"careme-pwa-v1"`,
		PrecacheURLs: string(precacheJSON),
		AuthPaths:    string(authJSON),
	}

	var buf bytes.Buffer
	if err := serviceWorkerTemplate.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
