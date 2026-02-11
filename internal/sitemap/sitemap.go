package sitemap

import (
	"encoding/xml"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"
)

type Server struct {
	mu   sync.RWMutex
	urls map[string]time.Time
}

func New() *Server {
	return &Server{urls: make(map[string]time.Time)}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
}

func (s *Server) TrackShoppingList(hash string) {
	if hash == "" {
		return
	}
	url := "/recipes?h=" + hash
	s.mu.Lock()
	s.urls[url] = time.Now().UTC()
	s.mu.Unlock()
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
	type record struct {
		loc     string
		lastmod time.Time
	}

	s.mu.RLock()
	records := make([]record, 0, len(s.urls))
	for loc, lastmod := range s.urls {
		records = append(records, record{loc: loc, lastmod: lastmod})
	}
	s.mu.RUnlock()

	sort.Slice(records, func(i, j int) bool {
		return records[i].loc < records[j].loc
	})

	entries := make([]urlEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, urlEntry{
			Loc:     record.loc,
			LastMod: record.lastmod.Format(time.RFC3339),
		})
	}

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
