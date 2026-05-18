package recipes

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"careme/internal/cache"
)

func AdminParamsJSON(c cache.Cache) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hash := r.PathValue("hash")
		if hash == "" {
			http.Error(w, "missing params hash", http.StatusBadRequest)
			return
		}

		params, err := IO(c).ParamsFromCache(r.Context(), hash)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				http.Error(w, "parameters not found in cache", http.StatusNotFound)
				return
			}
			slog.ErrorContext(r.Context(), "failed to load params for admin json", "hash", hash, "error", err)
			http.Error(w, "failed to load parameters", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(params); err != nil {
			slog.ErrorContext(r.Context(), "failed to encode params for admin json", "hash", hash, "error", err)
		}
	})
}
