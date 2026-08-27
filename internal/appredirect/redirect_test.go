package appredirect

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppURLForUserAgent(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		want      string
	}{
		{
			name:      "Android opens Google Play",
			userAgent: "Mozilla/5.0 (Linux; Android 16; Pixel 10) AppleWebKit/537.36 Chrome/140.0 Mobile Safari/537.36",
			want:      googlePlayAppURL,
		},
		{
			name:      "iPhone shows Apple coming soon",
			userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_6 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1",
			want:      appleComingSoonURL,
		},
		{
			name:      "iPad desktop user agent shows Apple coming soon",
			userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1",
			want:      appleComingSoonURL,
		},
		{
			name:      "Windows opens desktop PWA",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/140.0 Safari/537.36",
			want:      desktopPWAURL,
		},
		{
			name:      "Mac opens desktop PWA",
			userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.6 Safari/605.1.15",
			want:      desktopPWAURL,
		},
		{
			name: "unknown clients open desktop PWA",
			want: desktopPWAURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, appURLForUserAgent(tt.userAgent))
		})
	}
}

func TestRegister(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 16; Pixel 10) Mobile")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, googlePlayAppURL, rec.Header().Get("Location"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "User-Agent", rec.Header().Get("Vary"))
}
