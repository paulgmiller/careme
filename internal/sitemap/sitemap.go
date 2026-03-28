package sitemap

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"

	"careme/internal/cache"
	"careme/internal/recipes"
	"careme/internal/recipes/feedback"
	"careme/internal/routing"
)

type Server struct {
	cache        cache.ListCache
	publicOrigin string
}

const (
	robots = `# Allow all search engines to crawl the site
User-agent: *
Allow: /

# Sitemap location
Sitemap: %s/sitemap.xml
`
)

func New(c cache.ListCache, publicOrigin string) *Server {
	return &Server{
		cache:        c,
		publicOrigin: publicOrigin,
	}
}

func (s *Server) Register(mux routing.Registrar) {
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

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	shoppingListHashes, err := s.cache.List(r.Context(), recipes.ShoppingListCachePrefix, "")
	if err != nil {
		http.Error(w, "failed to load sitemap", http.StatusInternalServerError)
		slog.ErrorContext(r.Context(), "failed to read sitemap urls", "error", err)
		return
	}
	feedbackHashes, err := s.cache.List(r.Context(), feedback.RecipeFeedbackPrefix(), "")
	if err != nil {
		http.Error(w, "failed to load sitemap", http.StatusInternalServerError)
		slog.ErrorContext(r.Context(), "failed to read feedback urls", "error", err)
		return
	}

	entries := make([]urlEntry, 0, len(shoppingListHashes)+len(feedbackHashes)+1)
	entries = append(entries, urlEntry{Loc: s.publicOrigin + "/about"})

	// this is going to get too  big.  at some point we need a real db to find latest
	// or we track new entries and expire a lsit.
	for _, hash := range shoppingListHashes {
		entries = append(entries, urlEntry{Loc: s.publicOrigin + "/recipes?h=" + hash})
	}
	for _, hash := range feedbackHashes {
		exists, err := s.cache.Exists(r.Context(), recipes.SingleRecipeCacheKey(hash))
		if err != nil {
			http.Error(w, "failed to load sitemap", http.StatusInternalServerError)
			slog.ErrorContext(r.Context(), "failed to check recipe for feedback url", "hash", hash, "error", err)
			return
		}
		if !exists {
			continue
		}
		entries = append(entries, urlEntry{Loc: s.publicOrigin + "/recipe/" + hash})
	}
	slog.InfoContext(r.Context(), "serving sitemap with recipe urls", "count", len(entries), "shoppinglist_count", len(shoppingListHashes), "feedback_count", len(feedbackHashes))

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
	full := fmt.Sprintf(robots, s.publicOrigin)
	if _, err := w.Write([]byte(full)); err != nil {
		slog.ErrorContext(r.Context(), "failed to write robots.txt", "error", err)
	}
}
