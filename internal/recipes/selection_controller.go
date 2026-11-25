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

	if err := s.writeRecipeSelectionResponse(ctx, w, r, currentUser, recipeHash, *recipe, shoppingListHash, true); err != nil {
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
	if err := s.storage.SetRecipeSaved(currentUser, userRecipe(*recipe), true /* saved */); err != nil {
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
	if err := s.storage.SetRecipeSaved(currentUser, userRecipe(*recipe), false /* saved */); err != nil {
		slog.ErrorContext(ctx, "failed to record dismissed recipe", "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}

	if err := s.writeRecipeSelectionResponse(ctx, w, r, currentUser, recipeHash, *recipe, selectionHash, false); err != nil {
		slog.ErrorContext(ctx, "failed to render dismiss response", "hash", recipeHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

func (s *server) writeRecipeSelectionResponse(ctx context.Context, w http.ResponseWriter, r *http.Request, currentUser *utypes.User, recipeHash string, recipe ai.Recipe, shoppingListHash string, saved bool) error {
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

		params, err := s.ParamsFromCache(ctx, shoppingListHash)
		if err != nil {
			return fmt.Errorf("load shopping list parameters: %w", err)
		}
		view, err := s.shoppingListViewForRequest(r, shoppingListViewInput{params: params, hash: shoppingListHash, currentUser: currentUser})
		if err != nil {
			return err
		}
		if err := templates.ShoppingList.ExecuteTemplate(&response, "shopping_finalize_controls_response", view); err != nil {
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

func userRecipe(recipe ai.Recipe) utypes.Recipe {
	return utypes.Recipe{Title: recipe.Title, Hash: recipe.ComputeHash(), CreatedAt: time.Now()}
}

func writeShoppingRecipeCard(writer io.Writer, recipe ai.Recipe, state shoppingRecipeInput) error {
	view, err := newShoppingRecipeView(recipe, state)
	if err != nil {
		return fmt.Errorf("build shopping recipe card: %w", err)
	}
	return templates.ShoppingList.ExecuteTemplate(writer, "shopping_recipe_card", view)
}
