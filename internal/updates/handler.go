package updates

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"careme/internal/cache"
)

// FeedKey holds the published marketing feed in the existing recipes store.
const FeedKey = "updates/feed.json"

// Handler serves the published JSON feed from private storage.
func Handler(store cache.Cache) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/"+FeedKey {
			http.NotFound(w, r)
			return
		}
		reader, err := store.Get(r.Context(), FeedKey)
		if errors.Is(err, cache.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "read published update", "error", err)
			http.Error(w, "could not load update", http.StatusInternalServerError)
			return
		}
		defer func() { _ = reader.Close() }()
		data, err := io.ReadAll(reader)
		if err != nil {
			http.Error(w, "could not read update", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.ServeContent(w, r, "feed.json", time.Time{}, bytes.NewReader(data))
	})
}
