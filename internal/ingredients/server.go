package ingredients

import (
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/recipes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

type server struct {
	cache cache.Cache
}

func NewHandler(c cache.Cache) *server {
	return &server{cache: c}
}

func (s *server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /ingredients/{hash}", s.handleIngredients)
}

func (s *server) handleIngredients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := r.PathValue("hash")
	rio := recipes.IO(s.cache)

	params, err := rio.ParamsFromCache(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "parameters not found in cache", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load params for hash", "hash", hash, "error", err)
		http.Error(w, "failed to fetch params", http.StatusInternalServerError)
		return
	}

	locationHash := params.LocationHash()
	ingredients, err := rio.IngredientsFromCache(ctx, locationHash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "ingredients not found in cache", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load ingredients for hash", "hash", locationHash, "error", err)
		http.Error(w, "failed to fetch ingredients", http.StatusInternalServerError)
		return
	}

	slog.Info("serving cached ingredients", "location", params.String(), "hash", locationHash)
	if r.URL.Query().Get("format") == "tsv" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := kroger.ToTSV(ingredients, w); err != nil {
			http.Error(w, "failed to encode ingredients", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ingredients); err != nil {
		http.Error(w, "failed to encode ingredients", http.StatusInternalServerError)
		return
	}
}
