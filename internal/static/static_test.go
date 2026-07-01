package static

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"careme/internal/seasons"
)

func TestFaviconBySeason(t *testing.T) {
	tests := []struct {
		name   string
		season seasons.Season
		want   []byte
	}{
		{name: "fall", season: seasons.Fall, want: faviconFall},
		{name: "winter", season: seasons.Winter, want: faviconWinter},
		{name: "spring", season: seasons.Spring, want: faviconSpring},
		{name: "summer", season: seasons.Summer, want: faviconSummer},
		{name: "default falls back to fall", season: seasons.Season("unknown"), want: faviconFall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := faviconBySeason(tt.season)
			if len(got) == 0 {
				t.Fatal("favicon should not be empty")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("faviconBySeason(%q) length = %d, want %d", tt.season, len(got), len(tt.want))
			}
		})
	}
}

func TestRegisterServesPWAAssets(t *testing.T) {
	Init()

	mux := http.NewServeMux()
	Register(mux)

	tests := []struct {
		name        string
		path        string
		wantType    string
		wantSnippet string
	}{
		{
			name:        "manifest",
			path:        "/manifest.webmanifest",
			wantType:    "application/manifest+json; charset=utf-8",
			wantSnippet: `"display": "standalone"`,
		},
		{
			name:        "service worker",
			path:        "/sw.js",
			wantType:    "application/javascript; charset=utf-8",
			wantSnippet: TailwindAssetPath,
		},
		{
			name:        "offline page",
			path:        "/offline",
			wantType:    "text/html; charset=utf-8",
			wantSnippet: TailwindAssetPath,
		},
		{
			name:     "192 icon",
			path:     "/static/app-icon-192.png",
			wantType: "image/png",
		},
		{
			name:     "512 icon",
			path:     "/static/app-icon-512.png",
			wantType: "image/png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d", tt.path, rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantType {
				t.Fatalf("GET %s content type = %q, want %q", tt.path, got, tt.wantType)
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("GET %s returned empty body", tt.path)
			}
			if tt.wantSnippet != "" && !strings.Contains(rec.Body.String(), tt.wantSnippet) {
				t.Fatalf("GET %s body missing %q", tt.path, tt.wantSnippet)
			}
			if tt.path == "/offline" && !strings.Contains(rec.Body.String(), "Careme needs a connection.") {
				t.Fatalf("GET %s body missing offline copy", tt.path)
			}
		})
	}
}

func TestRegisterServesManifestNameByHost(t *testing.T) {
	Init()
	mux := http.NewServeMux()
	Register(mux)

	tests := []struct {
		name          string
		host          string
		wantName      string
		wantShortName string
	}{
		{
			name:          "production",
			host:          "careme.cooking",
			wantName:      "Careme",
			wantShortName: "Careme",
		},
		{
			name:          "test",
			host:          "test.careme.cooking",
			wantName:      "Careme Test",
			wantShortName: "Careme Test",
		},
		{
			name:          "test with port",
			host:          "test.careme.cooking:8080",
			wantName:      "Careme Test",
			wantShortName: "Careme Test",
		},
		{
			name:          "localhost",
			host:          "localhost:8080",
			wantName:      "Careme Local",
			wantShortName: "Careme Local",
		},
		{
			name:          "ipv4 localhost",
			host:          "127.0.0.1:8080",
			wantName:      "Careme Local",
			wantShortName: "Careme Local",
		},
		{
			name:          "ipv6 localhost",
			host:          "[::1]:8080",
			wantName:      "Careme Local",
			wantShortName: "Careme Local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("manifest response status = %d, want %d", rec.Code, http.StatusOK)
			}

			var manifest struct {
				Name      string `json:"name"`
				ShortName string `json:"short_name"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
				t.Fatalf("decode manifest: %v", err)
			}
			if manifest.Name != tt.wantName {
				t.Fatalf("manifest name = %q, want %q", manifest.Name, tt.wantName)
			}
			if manifest.ShortName != tt.wantShortName {
				t.Fatalf("manifest short_name = %q, want %q", manifest.ShortName, tt.wantShortName)
			}
		})
	}
}

func TestBackgroundBySeason(t *testing.T) {
	tests := []struct {
		name   string
		season seasons.Season
		want   []byte
	}{
		{name: "fall", season: seasons.Fall, want: backgroundFall},
		{name: "winter", season: seasons.Winter, want: backgroundWinter},
		{name: "spring", season: seasons.Spring, want: backgroundSpring},
		{name: "summer", season: seasons.Summer, want: backgroundSummer},
		{name: "default falls back to fall", season: seasons.Season("unknown"), want: backgroundFall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := backgroundBySeason(tt.season)
			if len(got) == 0 {
				t.Fatal("background should not be empty")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("backgroundBySeason(%q) length = %d, want %d", tt.season, len(got), len(tt.want))
			}
		})
	}
}

func TestServiceWorkerBypassesAuthRoutes(t *testing.T) {
	Init()
	var b strings.Builder
	err := renderServiceWorker(&b)
	if err != nil {
		t.Fatalf("renderServiceWorker() error = %v", err)
	}
	rendered := b.String()

	for _, path := range []string{"/sign-in", "/sign-up", "/auth/establish", "/logout"} {
		if !strings.Contains(rendered, path) {
			t.Fatalf("service worker should bypass %s, script: %s", path, rendered)
		}
	}

	if !strings.Contains(rendered, "Clerk redirects and auth bootstrap should always hit the network.") {
		t.Fatalf("service worker should keep inline comments for auth behavior, script: %s", rendered)
	}
}

func TestServiceWorkerRefreshesSeasonalFavicon(t *testing.T) {
	Init()
	var b strings.Builder
	err := renderServiceWorker(&b)
	if err != nil {
		t.Fatalf("renderServiceWorker() error = %v", err)
	}
	rendered := b.String()

	precacheURLs := serviceWorkerPrecacheURLs(t, rendered)
	for _, url := range precacheURLs {
		if url == "/favicon.ico" {
			t.Fatalf("service worker should not precache seasonal favicon, precache URLs: %v", precacheURLs)
		}
	}

	if !strings.Contains(rendered, `url.pathname === "/favicon.ico"`) {
		t.Fatalf("service worker should special-case favicon, script: %s", rendered)
	}
	if !strings.Contains(rendered, "event.respondWith(networkFirstWithCacheFallback(request))") {
		t.Fatalf("service worker should fetch favicon network-first, script: %s", rendered)
	}
	if !strings.Contains(rendered, "Favicons change with the season.") {
		t.Fatalf("service worker should document seasonal favicon cache behavior, script: %s", rendered)
	}
}

func serviceWorkerPrecacheURLs(t *testing.T, script string) []string {
	t.Helper()

	matches := regexp.MustCompile(`const PRECACHE_URLS = (\[[^\n]+\]);`).FindStringSubmatch(script)
	if len(matches) != 2 {
		t.Fatalf("service worker missing PRECACHE_URLS declaration: %s", script)
	}

	var urls []string
	if err := json.Unmarshal([]byte(matches[1]), &urls); err != nil {
		t.Fatalf("decode service worker precache URLs: %v", err)
	}
	return urls
}

func TestFontFilesEmbedded(t *testing.T) {
	matches, err := fs.Glob(fontFiles, "fonts/*.woff2")
	if err != nil {
		t.Fatalf("glob font files: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("embedded font file count = %d, want 2", len(matches))
	}
}

func TestRegisterServesFontFiles(t *testing.T) {
	Init()
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/static/fonts/inter-v20-latin-400-800.woff2", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("font response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "font/woff2" {
		t.Fatalf("font content type = %q, want %q", got, "font/woff2")
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("font cache control = %q", got)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("font response body should not be empty")
	}
}

func TestRegisterServesUserClerkBillingJS(t *testing.T) {
	Init()
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/static/user-clerk-billing.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("billing js response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("billing js content type = %q, want application/javascript; charset=utf-8", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("billing js cache control = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "mountPricingTable") {
		t.Fatal("billing js response should include Clerk pricing table mount logic")
	}
}

func TestRegisterServesSeasonalBackgroundFromEnv(t *testing.T) {
	t.Setenv(seasons.EnvSeason, "spring")
	Init()
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/background.png", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("background response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() != len(backgroundSpring) {
		t.Fatalf("background body length = %d, want spring length %d", rec.Body.Len(), len(backgroundSpring))
	}
}

func TestRegisterServesSeasonalFaviconFromEnv(t *testing.T) {
	t.Setenv(seasons.EnvSeason, "winter")
	Init()
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("favicon response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() != len(faviconWinter) {
		t.Fatalf("favicon body length = %d, want winter length %d", rec.Body.Len(), len(faviconWinter))
	}
}
