package updates

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"careme/internal/cache"
)

// Prefix holds public, published marketing content in the existing recipes store.
const Prefix = "updates/"

// Handler serves the archived posts and media from private storage.
func Handler(store cache.Cache) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/updates/post" {
			posts, err := ReadPosts(r.Context(), store)
			if errors.Is(err, cache.ErrNotFound) {
				servePost(w, r, nil)
				return
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "read published updates", "error", err)
				http.Error(w, "could not load updates", http.StatusInternalServerError)
				return
			}
			servePost(w, r, posts)
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/")
		if !strings.HasPrefix(key, Prefix) || path.Clean(key) != key {
			http.NotFound(w, r)
			return
		}
		reader, err := store.Get(r.Context(), key)
		if errors.Is(err, cache.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "read published update", "key", key, "error", err)
			http.Error(w, "could not load update", http.StatusInternalServerError)
			return
		}
		defer func() { _ = reader.Close() }()
		data, err := io.ReadAll(reader)
		if err != nil {
			http.Error(w, "could not read update", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(key)))
		w.Header().Set("Cache-Control", "public, max-age=300")
		if path.Ext(key) != ".json" && path.Ext(key) != ".md" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		// ServeContent provides byte ranges for native video playback and seeking.
		http.ServeContent(w, r, path.Base(key), time.Time{}, bytes.NewReader(data))
	})
}
