package recipes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/httpx"
	"careme/internal/routing"
	"careme/internal/templates"
	utypes "careme/internal/users/types"

	"github.com/samber/lo"
)

func (s *server) registerSelectionRoutes(mux routing.Registrar) {
	mux.HandleFunc("POST /recipe/{hash}/save", s.handleSaveRecipe)
	mux.HandleFunc("POST /recipe/{hash}/dismiss", s.handleDismissRecipe)
}

func (s *server) handleSaveRecipe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !httpx.IsHTMX(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	recipeHash := strings.TrimSpace(r.PathValue("hash"))
	if recipeHash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	shoppingListHash := strings.TrimSpace(r.FormValue(queryArgHash))
	if shoppingListHash == "" {
		http.Error(w, "recipe list hash not found", http.StatusBadRequest)
		return
	}
	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			returnTo := shoppingListArgs(map[string]string{
				queryArgHash: shoppingListHash,
			})
			redirectToAccountRequired(w, r, auth.AccountRequiredAddRecipe, returnTo)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for recipe save", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	recipe, err := s.saveRecipeForUser(ctx, currentUser, shoppingListHash, recipeHash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "recipe not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to save recipe", "shoppingListHash", shoppingListHash, "recipe_hash", recipeHash, "error", err)
		http.Error(w, "failed to save recipe", http.StatusInternalServerError)
		return
	}

	if err := s.writeRecipeSelectionResponse(ctx, w, r, recipeHash, *recipe, shoppingListHash, true); err != nil {
		slog.ErrorContext(ctx, "failed to render save response", "hash", recipeHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

func (s *server) saveRecipeForUser(ctx context.Context, currentUser *utypes.User, shoppingListHash, recipeHash string) (*ai.Recipe, error) {
	selection, err := s.loadRecipeSelection(ctx, currentUser.ID, shoppingListHash)
	if err != nil {
		return nil, fmt.Errorf("load recipe selection: %w", err)
	}
	selection.markSaved(recipeHash)
	if err := s.saveRecipeSelection(ctx, currentUser.ID, shoppingListHash, selection); err != nil {
		return nil, fmt.Errorf("save recipe selection: %w", err)
	}

	recipe, err := s.SingleFromCache(ctx, recipeHash)
	if err != nil {
		return nil, fmt.Errorf("load recipe: %w", err)
	}
	if err := s.saveRecipesToUserProfile(ctx, currentUser, *recipe); err != nil {
		return nil, fmt.Errorf("save recipe to user profile: %w", err)
	}

	params, err := s.ParamsFromCache(ctx, shoppingListHash)
	if err != nil {
		return nil, fmt.Errorf("load recipe params: %w", err)
	}
	s.startSavedRecipeBackgroundGeneration(ctx, recipeHash, *recipe, params.Location.ID, params.Date)

	return recipe, nil
}

func (s *server) handleDismissRecipe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !httpx.IsHTMX(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	recipeHash := strings.TrimSpace(r.PathValue("hash"))
	if recipeHash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			redirectToSignIn(w, r, http.StatusUnauthorized)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for recipe dismiss", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	selectionHash := strings.TrimSpace(r.FormValue(queryArgHash))
	if selectionHash == "" {
		http.Error(w, "recipe list hash not found", http.StatusBadRequest)
		return
	}
	selection, err := s.loadRecipeSelection(ctx, currentUser.ID, selectionHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load recipe selection for dismiss", "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}
	selection.markDismissed(recipeHash)
	if err := s.saveRecipeSelection(ctx, currentUser.ID, selectionHash, selection); err != nil {
		slog.ErrorContext(ctx, "failed to save recipe selection for dismiss", "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}

	if _, err := s.storage.RemoveRecipe(currentUser, recipeHash); err != nil {
		slog.ErrorContext(ctx, "failed to remove recipe from storage", "hash", recipeHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}

	recipe, recipeErr := s.SingleFromCache(ctx, recipeHash)
	if recipeErr != nil {
		if errors.Is(recipeErr, cache.ErrNotFound) {
			http.Error(w, "recipe not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe for dismiss response", "hash", recipeHash, "error", recipeErr)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}
	if err := s.writeRecipeSelectionResponse(ctx, w, r, recipeHash, *recipe, selectionHash, false); err != nil {
		slog.ErrorContext(ctx, "failed to render dismiss response", "hash", recipeHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

func (s *server) writeRecipeSelectionResponse(ctx context.Context, w http.ResponseWriter, r *http.Request, recipeHash string, recipe ai.Recipe, shoppingListHash string, saved bool) error {
	var response bytes.Buffer
	if isSingleRecipeAction(r) {
		if err := templates.Recipe.ExecuteTemplate(&response, "recipe_save_action", newRecipeSaveActionView(recipe, shoppingListHash, saved)); err != nil {
			return fmt.Errorf("render recipe save action: %w", err)
		}
	} else {
		if err := writeShoppingRecipeCard(&response, recipe, shoppingRecipeInput{
			Saved:              saved,
			Dismissed:          !saved,
			ShoppingListHash:   shoppingListHash,
			WineRecommendation: s.wineRecommendationForCard(ctx, recipeHash),
			HasImage:           s.recipeImageExistsForCard(ctx, recipeHash),
			ServerSignedIn:     true,
			Ready:              true,
		}); err != nil {
			return fmt.Errorf("render shopping recipe card: %w", err)
		}

		// Can finalize after any adds.
		if err := templates.ShoppingList.ExecuteTemplate(&response, "shopping_finalize_controls_response", newShoppingFinalizeView(shoppingListHash)); err != nil {
			return fmt.Errorf("render shopping finalize controls: %w", err)
		}

	}

	httpx.SetHTMLContentType(w)
	if saved {
		w.Header().Set("HX-Trigger", `{"careme:saved-recipes-changed":{},"careme:recipe-saved":{}}`)
	}
	if _, err := w.Write(response.Bytes()); err != nil {
		return fmt.Errorf("write recipe selection response: %w", err)
	}
	return nil
}

func (s *server) wineRecommendationForCard(ctx context.Context, recipeHash string) *ai.WineSelection {
	wineRecommendation, err := s.WineFromCache(ctx, recipeHash)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load cached wine recommendation for recipe card render", "recipe_hash", recipeHash, "error", err)
		}
		return nil
	}
	return wineRecommendation
}

func (s *server) recipeImageExistsForCard(ctx context.Context, recipeHash string) bool {
	exists, err := s.images.Exists(ctx, recipeHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to check cached recipe image for recipe card render", "recipe_hash", recipeHash, "error", err)
		return false
	}
	return exists
}

func (s *server) startSavedRecipeBackgroundGeneration(ctx context.Context, recipeHash string, recipe ai.Recipe, locationID string, date time.Time) {
	s.wg.Go(func() {
		bgctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		s.ensureSavedRecipeWine(bgctx, recipeHash, locationID, recipe, date)
	})
	s.wg.Go(func() {
		s.ensureRecipeImage(ctx, recipeHash, recipe)
	})
}

func (s *server) ensureSavedRecipeWine(ctx context.Context, recipeHash, locationID string, recipe ai.Recipe, date time.Time) {
	exists, err := s.WineExists(ctx, recipeHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to check cached wine selection after save", "hash", recipeHash, "error", err)
		return
	}
	if exists {
		return
	}
	slog.InfoContext(ctx, "generating wine picks on save", "hash", recipeHash)
	selection, err := s.generator.PickAWine(ctx, locationID, recipe, date)
	if err != nil {
		slog.ErrorContext(ctx, "failed to pick wine after save", "hash", recipeHash, "error", err)
		return
	}
	if err := s.SaveWine(ctx, recipeHash, selection); err != nil {
		slog.ErrorContext(ctx, "failed to save wine recommendation after save", "hash", recipeHash, "error", err)
	}
}

func (s *server) ensureRecipeImage(ctx context.Context, recipeHash string, recipe ai.Recipe) {
	// 4 minutes is a magical number here. neeed to look at data.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Minute)
	defer cancel()

	exists, err := s.images.Exists(ctx, recipeHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to check cached recipe image", "hash", recipeHash, "error", err)
		return
	}
	if exists {
		return
	}
	slog.InfoContext(ctx, "generating new recipe image", "hash", recipeHash)
	image, err := s.imagegen.GenerateRecipeImage(ctx, recipe)
	if err != nil {
		slog.ErrorContext(ctx, "failed to generate recipe image", "hash", recipeHash, "error", err)
		return
	}
	if err := s.images.Save(ctx, recipeHash, image); err != nil {
		slog.ErrorContext(ctx, "failed to save recipe image", "hash", recipeHash, "error", err)
	}
}

func isSingleRecipeAction(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.FormValue("source")), "recipe")
}

// saveRecipesToUserProfile adds saved recipes to the user's profile
func (s *server) saveRecipesToUserProfile(ctx context.Context, currentUser *utypes.User, recipe ai.Recipe) error {
	if currentUser == nil {
		return fmt.Errorf("invalid user")
	}

	// Check if the recipe already exists in the user's last recipes
	hash := recipe.ComputeHash()

	_, exists := lo.Find(currentUser.LastRecipes, func(r utypes.Recipe) bool {
		return r.Hash == hash
	})
	if exists {
		return nil
	}
	newRecipe := utypes.Recipe{
		Title:     recipe.Title,
		Hash:      hash,
		CreatedAt: time.Now(),
	}
	currentUser.LastRecipes = append(currentUser.LastRecipes, newRecipe)

	// etag mismatch fun!
	if err := s.storage.Update(currentUser); err != nil {
		return fmt.Errorf("failed to update user with saved recipes: %w", err)
	}
	slog.InfoContext(ctx, "added saved recipe to user profile", "title", recipe.Title)

	return nil
}

func writeShoppingRecipeCard(writer io.Writer, recipe ai.Recipe, state shoppingRecipeInput) error {
	view, err := newShoppingRecipeView(recipe, state)
	if err != nil {
		return fmt.Errorf("build shopping recipe card: %w", err)
	}
	return templates.ShoppingList.ExecuteTemplate(writer, "shopping_recipe_card", view)
}
