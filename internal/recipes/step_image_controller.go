package recipes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/templates"
)

type stepImageView struct {
	Hash           string
	Number         int
	HasImage       bool
	ServerSignedIn bool
}

func (s *server) recipeStepFromRequest(r *http.Request) (*ai.Recipe, int, int) {
	step, err := strconv.Atoi(r.PathValue("step"))
	if err != nil || step < 1 {
		return nil, 0, http.StatusNotFound
	}
	recipe, err := s.SingleFromCache(r.Context(), r.PathValue("hash"))
	if err != nil || step > len(recipe.Instructions) {
		return nil, 0, http.StatusNotFound
	}
	return recipe, step, 0
}

func (s *server) handleStepImage(w http.ResponseWriter, r *http.Request) {
	_, step, status := s.recipeStepFromRequest(r)
	if status != 0 {
		http.Error(w, "recipe step not found", status)
		return
	}
	s.serveCachedImage(w, r, stepImageID(r.PathValue("hash"), step))
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
	recipe, step, status := s.recipeStepFromRequest(r)
	if status != 0 {
		http.Error(w, "recipe step not found", status)
		return
	}
	hash := r.PathValue("hash")
	imageID := stepImageID(hash, step)
	// Serialize the cache check and generation to avoid duplicate requests from this server.
	s.stepImageMu.Lock()
	defer s.stepImageMu.Unlock()
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
