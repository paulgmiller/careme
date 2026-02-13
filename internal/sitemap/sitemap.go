package sitemap

import (
	"careme/internal/cache"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

type Server struct {
	cache cache.ListCache
}

const (
	domain = "https://careme.cooking"
	robots = `# Allow all search engines to crawl the site
User-agent: *
Allow: /

# Sitemap location
Sitemap: %s/sitemap.xml
`
)

func New(c cache.ListCache) *Server {

	return &Server{cache: c}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
}

type urlSet struct {
	XMLName xml.Name   `xml:"urlset"`
	Xmlns   string     `xml:"xmlns,attr"`
	URLs    []urlEntry `xml:"url"`
}

type urlEntry struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

func fnv64hash(hash string) bool {
	b, err := base64.URLEncoding.DecodeString(hash)
	if err != nil || len(b) != 14 {
		slog.Error("invalid hash in sitemap", "hash", hash, "error", err, "length", len(b))
		return false
	}
	return true
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	hashes, err := s.cache.List(r.Context(), "", "")
	if err != nil {
		http.Error(w, "failed to load sitemap", http.StatusInternalServerError)
		slog.ErrorContext(r.Context(), "failed to read sitemap urls", "error", err)
		return
	}
	entries := make([]urlEntry, 0, len(hashes))

	//this is going to get too  big.  at some point we need a real db to find latest
	//or we track new entries and expire a lsit.
	for _, hash := range hashes {
		if hash == "" || strings.Contains(hash, "/") || strings.HasSuffix(hash, ".params") {
			continue
		}
		if !fnv64hash(hash) {
			continue
		}
		entries = append(entries, urlEntry{Loc: domain + "/recipes?h=" + hash})
	}
	slog.InfoContext(r.Context(), "serving sitemap with recipe urls", "count", len(entries), "blobcount", len(hashes))

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	if _, err := w.Write([]byte(xml.Header)); err != nil {
		slog.ErrorContext(r.Context(), "failed to write sitemap header", "error", err)
		return
	}
	if err := xml.NewEncoder(w).Encode(urlSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  entries,
	}); err != nil {
		slog.ErrorContext(r.Context(), "failed to encode sitemap", "error", err)
	}
}

func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	full := fmt.Sprintf(robots, domain)
	if _, err := w.Write([]byte(full)); err != nil {
		slog.ErrorContext(r.Context(), "failed to write robots.txt", "error", err)
	}
}
