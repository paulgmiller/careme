package ingredients

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/recipes"
	"careme/internal/routing"
)

type server struct {
	cache cache.Cache
}

func NewHandler(c cache.Cache) *server {
	return &server{cache: c}
}

func (s *server) Register(mux routing.Registrar) {
	mux.HandleFunc("GET /ingredients/{hash}", s.handleIngredients)
}

func (s *server) handleIngredients(w http.ResponseWriter, r *http.Request) {
	ingredients, err := s.loadCachedIngredients(r)
	if err != nil {
		s.writeIngredientLoadError(w, r, err)
		return
	}

	slices.SortFunc(ingredients, func(a, b ai.InputIngredient) int {
		if a.Grade == nil || b.Grade == nil {
			return strings.Compare(strings.ToLower(a.Description), strings.ToLower(b.Description))
		}
		return b.Grade.Score - a.Grade.Score
	})

	slog.Info("serving cached ingredients", "path", r.URL.Path)
	if r.URL.Query().Get("format") == "tsv" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := ai.InputIngredientsToTSV(ingredients, w); err != nil {
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

func (s *server) loadCachedIngredients(r *http.Request) ([]ai.InputIngredient, error) {
	ctx := r.Context()
	hash := r.PathValue("hash")
	locationHash, err := s.loadLocationHash(ctx, hash)
	if err != nil {
		return nil, err
	}

	rio := recipes.IO(s.cache)
	ingredients, err := rio.IngredientsFromCache(ctx, locationHash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, cache.ErrNotFound
		}
		slog.ErrorContext(ctx, "failed to load ingredients for hash", "hash", locationHash, "error", err)
		return nil, err
	}
	return ingredients, nil
}

func (s *server) loadLocationHash(ctx context.Context, hash string) (string, error) {
	rio := recipes.IO(s.cache)
	params, err := rio.ParamsFromCache(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return "", cache.ErrNotFound
		}
		slog.ErrorContext(ctx, "failed to load params for hash", "hash", hash, "error", err)
		return "", err
	}
	return params.LocationHash(), nil
}

func (s *server) writeIngredientLoadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, cache.ErrNotFound):
		if _, paramsErr := recipes.IO(s.cache).ParamsFromCache(r.Context(), r.PathValue("hash")); errors.Is(paramsErr, cache.ErrNotFound) {
			http.Error(w, "parameters not found in cache", http.StatusNotFound)
			return
		}
		http.Error(w, "ingredients not found in cache", http.StatusNotFound)
	default:
		http.Error(w, "failed to fetch ingredients", http.StatusInternalServerError)
	}
}
