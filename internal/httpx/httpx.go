package httpx

import (
	"net/http"
	"net/url"
	"strings"
)

const htmlContentType = "text/html; charset=utf-8"

// SetHTMLContentType marks a response as UTF-8 HTML.
func SetHTMLContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", htmlContentType)
}

// IsHTMX reports whether the request was issued by HTMX.
func IsHTMX(r *http.Request) bool {
	return r != nil && strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

// RequestPath returns the request URI, falling back to the path and then "/".
func RequestPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/"
	}
	if uri := strings.TrimSpace(r.URL.RequestURI()); uri != "" {
		return uri
	}
	if path := strings.TrimSpace(r.URL.Path); path != "" {
		return path
	}
	return "/"
}

// LocalReferrerPath returns a local referring path, or "/" when the referrer
// is missing, malformed, or belongs to another host.
func LocalReferrerPath(r *http.Request) string {
	if r == nil {
		return "/"
	}
	referrer, err := url.Parse(strings.TrimSpace(r.Referer()))
	if err != nil || referrer == nil {
		return "/"
	}
	if referrer.IsAbs() && !strings.EqualFold(referrer.Host, r.Host) {
		return "/"
	}
	if referrer.Path == "" || !strings.HasPrefix(referrer.Path, "/") {
		return "/"
	}
	return referrer.RequestURI()
}
