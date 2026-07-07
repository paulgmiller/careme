package static

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	texttemplate "text/template"

	"careme/internal/routing"
	"careme/internal/seasons"
)

//go:embed app-icon-192.png
var appIcon192 []byte

//go:embed app-icon-512.png
var appIcon512 []byte

//go:embed manifest.webmanifest
var manifestWebmanifest []byte

//go:embed assetlinks.json
var assetLinksJSON []byte

//go:embed offline.html
var offlineHTML []byte

//go:embed sw.js.tmpl
var serviceWorkerJS []byte

var (
	manifestTemplate      = texttemplate.Must(texttemplate.New("manifest").Parse(string(manifestWebmanifest)))
	offlinePageTemplate   = template.Must(template.New("offline").Parse(string(offlineHTML)))
	serviceWorkerTemplate = texttemplate.Must(texttemplate.New("sw").Parse(string(serviceWorkerJS)))
)

func registerPWAAssets(mux routing.Registrar) {
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
		err := renderManifest(r.Host, w)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to render web manifest", "error", err)
			http.Error(w, "manifest error", http.StatusInternalServerError)
			return
		}
	})

	mux.HandleFunc("/.well-known/assetlinks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		if _, err := w.Write(assetLinksJSON); err != nil {
			slog.ErrorContext(r.Context(), "failed to write Android asset links", "error", err)
		}
	})

	mux.HandleFunc("/offline", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		err := renderOfflinePage(w)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to render offline page", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
	})

	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		err := renderServiceWorker(w)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to render service worker", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
	})
}

func renderManifest(host string, w io.Writer) error {
	name := "Careme"
	shortName := "Careme"
	hostname := manifestHostname(host)

	switch {
	case strings.EqualFold(hostname, "test.careme.cooking"):
		name = "Careme Test"
		shortName = "Careme Test"
	case strings.EqualFold(hostname, "localhost"), hostname == "127.0.0.1", hostname == "::1":
		name = "Careme Local"
		shortName = "Careme Local"
	}

	data := struct {
		Name      string
		ShortName string
	}{
		Name:      name,
		ShortName: shortName,
	}
	return manifestTemplate.Execute(w, data)
}

func manifestHostname(host string) string {
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		return strings.Trim(host, "[]")
	}
	return hostname
}

func renderOfflinePage(w io.Writer) error {
	scheme := seasons.GetCurrentColorScheme()
	data := struct {
		TailwindAssetPath string
		ThemeColor        string
		Colors            seasons.ColorScheme
	}{
		TailwindAssetPath: TailwindAssetPath,
		ThemeColor:        scheme.C50,
		Colors:            scheme,
	}

	return offlinePageTemplate.Execute(w, data)
}

func renderServiceWorker(w io.Writer) error {
	precachePaths := []string{
		"/offline",
		"/manifest.webmanifest",
		"/static/app-icon-192.png",
		"/static/app-icon-512.png",
		"/static/htmx@2.0.8.js",
		TailwindAssetPath,
	}
	authPaths := []string{"/sign-in", "/sign-up", "/auth/establish", "/logout"}

	precacheJSON, err := json.Marshal(precachePaths)
	if err != nil {
		return err
	}
	authJSON, err := json.Marshal(authPaths)
	if err != nil {
		return err
	}
	cacheName, err := serviceWorkerCacheName(precachePaths, authPaths)
	if err != nil {
		return err
	}

	data := struct {
		CacheName    string
		PrecacheURLs string
		AuthPaths    string
	}{
		CacheName:    cacheName,
		PrecacheURLs: string(precacheJSON),
		AuthPaths:    string(authJSON),
	}

	return serviceWorkerTemplate.Execute(w, data)
}

func serviceWorkerCacheName(precachePaths, authPaths []string) (string, error) {
	var offline bytes.Buffer
	if err := renderOfflinePage(&offline); err != nil {
		return "", err
	}

	hash := sha256.New()
	for _, part := range [][]byte{
		[]byte(strings.Join(precachePaths, "\x00")),
		[]byte(strings.Join(authPaths, "\x00")),
		manifestWebmanifest,
		offline.Bytes(),
		appIcon192,
		appIcon512,
		htmx208JS,
		[]byte(TailwindAssetPath),
	} {
		if _, err := fmt.Fprintf(hash, "%d:", len(part)); err != nil {
			return "", err
		}
		if _, err := hash.Write(part); err != nil {
			return "", err
		}
	}

	cacheName := fmt.Sprintf("careme-pwa-%x", hash.Sum(nil)[:8])
	cacheNameJSON, err := json.Marshal(cacheName)
	if err != nil {
		return "", err
	}
	return string(cacheNameJSON), nil
}
