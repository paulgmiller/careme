package sitemap

import (
	"encoding/xml"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

type record struct {
	loc     string
	lastmod time.Time
}

type op struct {
	trackHash string
	snapshot  chan []record
}

type Server struct {
	ops chan op
}

func New() *Server {
	s := &Server{ops: make(chan op)}
	go s.run()
	return s
}

func (s *Server) run() {
	urls := make(map[string]time.Time)
	for msg := range s.ops {
		switch {
		case msg.trackHash != "":
			url := "/recipes?h=" + msg.trackHash
			urls[url] = time.Now().UTC()
		case msg.snapshot != nil:
			records := make([]record, 0, len(urls))
			for loc, lastmod := range urls {
				records = append(records, record{loc: loc, lastmod: lastmod})
			}
			msg.snapshot <- records
		}
	}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
}

func (s *Server) TrackShoppingList(hash string) {
	if hash == "" {
		return
	}
	s.ops <- op{trackHash: hash}
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

func (s *Server) snapshot() []record {
	out := make(chan []record, 1)
	s.ops <- op{snapshot: out}
	return <-out
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	records := s.snapshot()

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
