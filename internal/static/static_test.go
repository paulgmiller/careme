package static

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
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
