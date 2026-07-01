package static

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

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
