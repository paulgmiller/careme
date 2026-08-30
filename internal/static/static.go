package static

import (
	"embed"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"

	"careme/internal/routing"
	"careme/internal/seasons"
)

//go:embed tailwind.css
var tailwindCSS []byte

//go:embed htmx@2.0.8.js
var htmx208JS []byte

//go:embed user-clerk-billing.js
var userClerkBillingJS []byte

//go:embed share.js
var shareJS []byte

//go:embed recipe.js
var recipeJS []byte

//go:embed farmersmarket.js
var farmersMarketJS []byte

//go:embed fonts/*.woff2
var fontFiles embed.FS

//go:embed favicon-fall.png
var faviconFall []byte

//go:embed favicon-winter.png
var faviconWinter []byte

//go:embed favicon-spring.png
var faviconSpring []byte

//go:embed favicon-summer.png
var faviconSummer []byte

//go:embed fall.webp
var backgroundFall []byte

//go:embed winter.webp
var backgroundWinter []byte

//go:embed spring.webp
var backgroundSpring []byte

//go:embed summer.webp
var backgroundSummer []byte

var AssetPath string

func Init() {
	hasher := fnv.New64()

	for _, asset := range [][]byte{tailwindCSS, userClerkBillingJS, shareJS, recipeJS, farmersMarketJS} {
		hasher.Write(asset)
	}
	AssetPath = fmt.Sprintf("/static/%x/", hasher.Sum(nil))
}

const immutable = "public, max-age=31536000, immutable"

// helper for immutable I bett http package has something like this.
// could embed whole fs and then use http.FileServerFS()
func static(contentType string, buf []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", immutable)
		if _, err := w.Write(buf); err != nil {
			slog.ErrorContext(r.Context(), "failed to write tailwind css", "error", err)
		}
	}
}

const jsContentType = "application/javascript; charset=utf-8"

// Register serves static assets and wires template asset paths.
func Register(mux routing.Registrar) {
	mux.HandleFunc(AssetPath+"tailwind.css", static("text/css; charset=utf-8", tailwindCSS))

	// Intentionally versioned so that we can cache aggressively.
	mux.HandleFunc("/static/htmx@2.0.8.js", static(jsContentType, htmx208JS))

	// bad form to redirect to assetpath so pages are simpler? Still have to do head requests
	mux.HandleFunc(AssetPath+"user-clerk-billing.js", static(jsContentType, userClerkBillingJS))
	// w.Header().Set("Cache-Control", "public, max-age=3600")

	mux.HandleFunc(AssetPath+"share.js", static(jsContentType, shareJS))
	mux.HandleFunc(AssetPath+"recipe.js", static(jsContentType, recipeJS))

	mux.HandleFunc(AssetPath+"farmersmarket.js", static(jsContentType, farmersMarketJS))

	fontServer := http.FileServer(http.FS(fontFiles))
	mux.Handle("/static/fonts/", http.StripPrefix("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "font/woff2")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fontServer.ServeHTTP(w, r)
	})))

	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// Keep cache short so clients can refresh seasonally without manual cache clear.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		favicon := faviconBySeason(seasons.GetCurrentSeason())
		if _, err := w.Write(favicon); err != nil {
			slog.ErrorContext(r.Context(), "failed to write favicon", "error", err)
		}
	})

	registerPWAAssets(mux)

	mux.HandleFunc("/background.webp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		// Keep cache short so clients can refresh seasonally without manual cache clear.
		// could redirect to
		w.Header().Set("Cache-Control", "public, max-age=3600")
		background := backgroundBySeason(seasons.GetCurrentSeason())
		if _, err := w.Write(background); err != nil {
			slog.ErrorContext(r.Context(), "failed to write seasonal background", "error", err)
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

func backgroundBySeason(season seasons.Season) []byte {
	switch season {
	case seasons.Winter:
		return backgroundWinter
	case seasons.Spring:
		return backgroundSpring
	case seasons.Summer:
		return backgroundSummer
	case seasons.Fall:
		fallthrough
	default:
		return backgroundFall
	}
}
