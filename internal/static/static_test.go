package static

import (
	"net/http"
	"net/http/httptest"
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
			wantSnippet: "Careme needs a connection.",
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
		})
	}
}

func TestServiceWorkerBypassesAuthRoutes(t *testing.T) {
	script := serviceWorkerScript()

	for _, path := range []string{"/sign-in", "/sign-up", "/auth/establish", "/logout"} {
		if !strings.Contains(script, path) {
			t.Fatalf("service worker should bypass %s, script: %s", path, script)
		}
	}
}
