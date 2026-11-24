package updates

import (
	"bytes"
	"encoding/json"
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
		// The stored manifest retains all history; the page only fetches ten posts.
		if key == Prefix+"feed.json" {
			var posts []Post
			if err := json.Unmarshal(data, &posts); err != nil {
				http.Error(w, "could not read feed", http.StatusInternalServerError)
				return
			}
			data, err = json.Marshal(posts[:min(10, len(posts))])
			if err != nil {
				http.Error(w, "could not encode feed", http.StatusInternalServerError)
				return
			}
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
