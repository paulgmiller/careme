package critique

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/seasons"
	"careme/internal/templates"
)

type recipeio interface {
	SingleFromCache(context.Context, string) (*ai.Recipe, error)
}

func CritiquePage(s store, rio recipeio) http.Handler {
	if rio == nil {
		panic("store and recipeio must not be nil")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		hash := r.PathValue("hash")
		if hash == "" {
			http.Error(w, "missing recipe hash", http.StatusBadRequest)
			return
		}
		cachedCritique, err := s.Load(r.Context(), hash)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				http.Error(w, "critique not found", http.StatusNotFound)
				return
			}
			slog.ErrorContext(r.Context(), "failed to load recipe critique", "hash", hash, "error", err)
			http.Error(w, "unable to load critique", http.StatusInternalServerError)
			return
		}

		recipeTitle := "Recipe"
		if recipe, err := rio.SingleFromCache(r.Context(), hash); err == nil {
			recipeTitle = recipe.Title
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := templates.Critique.Execute(w, struct {
			RecipeTitle     string
			RecipeURL       string
			ClarityScript   template.HTML
			GoogleTagScript template.HTML
			Style           seasons.Style
			ai.RecipeCritique
		}{
			RecipeTitle:     recipeTitle,
			RecipeURL:       "/recipe/" + hash,
			ClarityScript:   templates.ClarityScript(r.Context()),
			GoogleTagScript: templates.GoogleTagScript(),
			Style:           seasons.GetCurrentStyle(),
			RecipeCritique:  *cachedCritique,
		}); err != nil {
			slog.ErrorContext(r.Context(), "failed to render recipe critique page", "hash", hash, "error", err)
			http.Error(w, "unable to render critique", http.StatusInternalServerError)
			return
		}
	})
}
