package recipes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/templates"
)

type stepImageView struct {
	Hash           string
	Number         int
	HasImage       bool
	ServerSignedIn bool
}

func (s *server) recipeStepFromRequest(r *http.Request) (hash string, step int) {
	step, err := strconv.Atoi(r.PathValue("step"))
	if err != nil {
		return "", -1
	}
	return r.PathValue("hash"), step
}

func (s *server) handleStepImage(w http.ResponseWriter, r *http.Request) {
	hash, step := s.recipeStepFromRequest(r)
	if step < 1 {
		http.Error(w, "bad step", http.StatusNotFound)
		return
	}
	s.serveCachedImage(w, r, stepImageID(hash, step))
}

func (s *server) handleGenerateStepImage(w http.ResponseWriter, r *http.Request) {
	_, err := s.clerk.GetUserIDFromRequest(r)
	if errors.Is(err, auth.ErrNoSession) {
		http.Error(w, "sign in to generate an illustration", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "unable to verify account", http.StatusInternalServerError)
		return
	}
	hash, step := s.recipeStepFromRequest(r)
	if step < 1 {
		http.Error(w, "bad step", http.StatusNotFound)
		return
	}

	recipe, err := s.SingleFromCache(r.Context(), hash)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, cache.ErrNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, "could not fetch recipe", status)
		return
	}

	if step > len(recipe.Instructions) {
		http.Error(w, "bad step", http.StatusNotFound)
		return
	}
	imageID := stepImageID(hash, step)
	// use a job id to prevent double generation? Hard to do through ui but easy with posts.
	exists, err := s.images.Exists(r.Context(), imageID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to check step image", "image", imageID, "error", err)
		http.Error(w, "unable to check illustration", http.StatusInternalServerError)
		return
	}
	if !exists {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		image, err := s.imagegen.GenerateStepImage(ctx, *recipe, step)
		if err != nil {
			slog.ErrorContext(ctx, "failed to generate step image", "image", imageID, "error", err)
			http.Error(w, "unable to generate illustration", http.StatusBadGateway)
			return
		}
		if err := s.images.Save(ctx, imageID, image); err != nil {
			slog.ErrorContext(ctx, "failed to save step image", "image", imageID, "error", err)
			http.Error(w, "unable to save illustration", http.StatusInternalServerError)
			return
		}
	}
	renderHTML(w, templates.Recipe, "step_image", stepImageView{Hash: hash, Number: step, HasImage: true, ServerSignedIn: true})
}
