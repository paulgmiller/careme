package static

import (
	"fmt"
	"io/fs"
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
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, AssetPath+"user-clerk-billing.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		fmt.Println(AssetPath)

		t.Fatalf("billing js response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("billing js content type = %q, want application/javascript; charset=utf-8", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != immutable {
		t.Fatalf("billing js cache control = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "mountPricingTable") {
		t.Fatal("billing js response should include Clerk pricing table mount logic")
	}
}

func TestRegisterServesShareJS(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, AssetPath+"share.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("share js response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("share js content type = %q, want application/javascript; charset=utf-8", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != immutable {
		t.Fatalf("share js cache control = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "navigator.share") {
		t.Fatal("share js response should include Web Share logic")
	}
}

func TestRegisterServesRecipeJS(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, AssetPath+"recipe.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("recipe js response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("recipe js content type = %q, want application/javascript; charset=utf-8", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != immutable {
		t.Fatalf("recipe js cache control = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "initializeRecipeSteps") {
		t.Fatal("recipe js response should include recipe step interaction logic")
	}
	if !strings.Contains(rec.Body.String(), `event.pointerType !== "touch"`) ||
		!strings.Contains(rec.Body.String(), `event.pointerType !== "pen"`) {
		t.Fatal("recipe step swiping should only start for touch or pen pointers")
	}
	if !strings.Contains(rec.Body.String(), "data-recipe-step-done") {
		t.Fatal("recipe js should support clicking a step number to complete it")
	}
}

func TestRegisterServesFarmersMarketJS(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, AssetPath+"farmersmarket.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("farmers market js response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("farmers market js content type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != immutable {
		t.Fatalf("farmers market js cache control = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "Compressor") {
		t.Fatal("farmers market js should include image compression logic")
	}
}

func TestRegisterServesSeasonalBackgroundFromEnv(t *testing.T) {
	t.Setenv(seasons.EnvSeason, "spring")
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/background.webp", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("background response status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/webp" {
		t.Fatalf("background content type = %q, want image/webp", got)
	}
	if rec.Body.Len() != len(backgroundSpring) {
		t.Fatalf("background body length = %d, want spring length %d", rec.Body.Len(), len(backgroundSpring))
	}
}

func TestRegisterServesSeasonalFaviconFromEnv(t *testing.T) {
	t.Setenv(seasons.EnvSeason, "winter")
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
