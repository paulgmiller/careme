package sitemap

import (
	"bufio"
	"bytes"
	"careme/internal/cache"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

const (
	sitemapContainer = "recipes"
	sitemapBlobName  = "sitemap/urls.ndjson"
)

type appendBlobStore struct {
}

type sitemapEntry struct {
	URL     string    `json:"url"`
	LastMod time.Time `json:"lastmod"`
}

type Server struct {
	cache cache.ListCache
}

func New(c cache.ListCache) *Server {

	return &Server{cache: c}
}

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
}

func (s *Server) TrackShoppingList(hash string) {
	if hash == "" {
		return
	}
	entry := sitemapEntry{URL: "/recipes?h=" + hash, LastMod: time.Now().UTC()}
	if err := s.store.Append(context.Background(), entry); err != nil {
		slog.Error("failed to append sitemap url", "error", err, "url", entry.URL)
	}
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
	records, err := s.store.ReadAll(r.Context())
	if err != nil {
		http.Error(w, "failed to load sitemap", http.StatusInternalServerError)
		slog.ErrorContext(r.Context(), "failed to read sitemap urls", "error", err)
		return
	}

	latest := make(map[string]time.Time, len(records))
	for _, record := range records {
		if record.URL == "" {
			continue
		}
		if ts, ok := latest[record.URL]; !ok || record.LastMod.After(ts) {
			latest[record.URL] = record.LastMod
		}
	}

	entries := make([]urlEntry, 0, len(latest))
	for loc, lastmod := range latest {
		entries = append(entries, urlEntry{Loc: loc, LastMod: lastmod.Format(time.RFC3339)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Loc < entries[j].Loc })

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

func (a *appendBlobStore) Append(ctx context.Context, entry sitemapEntry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	_, err = a.blob.AppendBlock(ctx, readSeekNopCloser{bytes.NewReader(payload)}, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			if _, createErr := a.blob.Create(ctx, nil); createErr != nil && !bloberror.HasCode(createErr, bloberror.BlobAlreadyExists) {
				return createErr
			}
			_, err = a.blob.AppendBlock(ctx, readSeekNopCloser{bytes.NewReader(payload)}, nil)
		}
	}
	return err
}

func (a *appendBlobStore) ReadAll(ctx context.Context) ([]sitemapEntry, error) {
	resp, err := a.blob.DownloadStream(ctx, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "failed to close sitemap blob stream", "error", closeErr)
		}
	}()
	return parseEntries(resp.Body)
}

func parseEntries(r io.Reader) ([]sitemapEntry, error) {
	entries := make([]sitemapEntry, 0)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry sitemapEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

type disabledStore struct{}

func (disabledStore) Append(_ context.Context, _ sitemapEntry) error {
	return errors.New("append blob store is disabled")
}

func (disabledStore) ReadAll(_ context.Context) ([]sitemapEntry, error) {
	return nil, nil
}

type readSeekNopCloser struct{ io.ReadSeeker }

func (r readSeekNopCloser) Close() error { return nil }
