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
		s.handleLeftoversSelection(w, r, recipeHash, true)
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
		s.handleLeftoversSelection(w, r, recipeHash, false)
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
	wineRecommendation, err := s.wineFromCache(ctx, recipeHash)
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

// Leftovers use the same Save action and profile history without a shopping list.
func (s *server) handleLeftoversSelection(w http.ResponseWriter, r *http.Request, hash string, save bool) {
	recipe, err := s.SingleFromCache(r.Context(), hash)
	if err != nil || !recipe.Leftovers {
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "unable to load recipe", http.StatusInternalServerError)
		} else {
			http.Error(w, "recipe not found", http.StatusNotFound)
		}
		return
	}
	user, err := s.storage.FromRequest(r.Context(), r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			redirectToAccountRequired(w, r, auth.AccountRequiredAddRecipe, "/recipe/"+hash)
		} else {
			http.Error(w, "unable to load account", http.StatusInternalServerError)
		}
		return
	}
	if save {
		err = s.saveRecipesToUserProfile(r.Context(), user, *recipe)
	} else {
		_, err = s.storage.RemoveRecipe(user, hash)
	}
	if err != nil {
		http.Error(w, "unable to save recipe decision", http.StatusInternalServerError)
		return
	}
	httpx.SetHTMLContentType(w)
	if save {
		w.Header().Set("HX-Trigger", `{"careme:saved-recipes-changed":{},"careme:recipe-saved":{}}`)
	}
	if err := templates.Recipe.ExecuteTemplate(w, "recipe_save_action", newRecipeSaveActionView(*recipe, "", save)); err != nil {
		slog.ErrorContext(r.Context(), "render leftovers save action", "error", err)
	}
}
