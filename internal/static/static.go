package static

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

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
		if _, err := w.Write(offlineHTML); err != nil {
			slog.ErrorContext(r.Context(), "failed to write offline page", "error", err)
		}
	})

	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if _, err := w.Write([]byte(serviceWorkerScript())); err != nil {
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

func serviceWorkerScript() string {
	precachePaths := []string{
		"/offline",
		"/manifest.webmanifest",
		"/favicon.ico",
		"/static/app-icon-192.png",
		"/static/app-icon-512.png",
		"/static/htmx@2.0.8.js",
		TailwindAssetPath,
	}

	quotedPaths := make([]string, 0, len(precachePaths))
	for _, path := range precachePaths {
		quotedPaths = append(quotedPaths, strconv.Quote(path))
	}

	return fmt.Sprintf(`const CACHE_NAME = "careme-pwa-v1";
const PRECACHE_URLS = [%s];
const AUTH_PATHS = new Set(["/sign-in", "/sign-up", "/auth/establish", "/logout"]);

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(PRECACHE_URLS)).then(() => self.skipWaiting()),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key)),
      ),
    ).then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") {
    return;
  }

  const url = new URL(request.url);
  if (url.origin !== self.location.origin) {
    return;
  }
  if (AUTH_PATHS.has(url.pathname)) {
    return;
  }

  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request).catch(() => caches.match("/offline")),
    );
    return;
  }

  const isStaticAsset = url.pathname.startsWith("/static/") ||
    url.pathname === "/favicon.ico" ||
    url.pathname === "/manifest.webmanifest" ||
    url.pathname === "/offline";
  if (!isStaticAsset) {
    return;
  }

  event.respondWith(
    caches.match(request).then((cached) => {
      if (cached) {
        return cached;
      }

      return fetch(request).then((response) => {
        if (!response || !response.ok) {
          return response;
        }

        const responseCopy = response.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(request, responseCopy));
        return response;
      });
    }),
  );
});
`, joinQuoted(quotedPaths))
}

func joinQuoted(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	result := paths[0]
	for _, path := range paths[1:] {
		result += ", " + path
	}
	return result
}
