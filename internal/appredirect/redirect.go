package appredirect

import (
	"net/http"
	"strings"

	"careme/internal/routing"
)

const (
	googlePlayAppURL   = "https://play.google.com/store/apps/details?id=cooking.careme"
	appleComingSoonURL = "/about#install"
	desktopPWAURL      = "/"
)

// Register adds the platform-aware app destination route.
func Register(routes routing.Registrar) {
	routes.HandleFunc("GET /app", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Vary", "User-Agent")
		http.Redirect(w, r, appURLForUserAgent(r.UserAgent()), http.StatusFound)
	})
}

func appURLForUserAgent(userAgent string) string {
	userAgent = strings.ToLower(userAgent)

	switch {
	case strings.Contains(userAgent, "android"):
		return googlePlayAppURL
	case strings.Contains(userAgent, "iphone"),
		strings.Contains(userAgent, "ipad"),
		strings.Contains(userAgent, "ipod"),
		strings.Contains(userAgent, "macintosh") && strings.Contains(userAgent, "mobile/"):
		return appleComingSoonURL
	default:
		return desktopPWAURL
	}
}
