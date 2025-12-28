package recipes

import (
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	"careme/internal/users"
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
)

// UpdateShoppingList rebuilds the shopping list from all saved recipes
func (s *server) UpdateShoppingList(ctx context.Context, userID string) error {
	if userID == "" {
		return errors.New("invalid user")
	}

	currentUser, err := s.storage.GetByID(userID)
	if err != nil {
		return err
	}

	// Clear the existing shopping list
	currentUser.ShoppingList = []users.ShoppingListItem{}

	// Iterate through all saved recipes and aggregate ingredients
	for _, savedRecipe := range currentUser.LastRecipes {
		// Load the recipe from cache
		recipe, err := s.SingleFromCache(ctx, savedRecipe.Hash)
		if err != nil {
			slog.WarnContext(ctx, "failed to load recipe for shopping list", "hash", savedRecipe.Hash, "error", err)
			continue
		}

		// Add ingredients from this recipe to shopping list
		for _, ingredient := range recipe.Ingredients {
			item := users.ShoppingListItem{
				Name:         ingredient.Name,
				Quantity:     ingredient.Quantity,
				Price:        ingredient.Price,
				RecipeTitle:  recipe.Title,
				// Aisle info will be added if available
			}
			currentUser.ShoppingList = append(currentUser.ShoppingList, item)
		}
	}

	// Save the updated user
	if err := s.storage.Update(currentUser); err != nil {
		return err
	}

	return nil
}

// handleShoppingList serves the shopping list page
func (s *server) handleShoppingList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser, err := users.FromRequest(r, s.storage)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			users.ClearCookie(w)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for shopping list", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	if currentUser == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Rebuild shopping list from saved recipes
	if err := s.UpdateShoppingList(ctx, currentUser.ID); err != nil {
		slog.ErrorContext(ctx, "failed to update shopping list", "error", err)
		http.Error(w, "failed to update shopping list", http.StatusInternalServerError)
		return
	}

	// Reload user to get the updated shopping list
	currentUser, err = s.storage.GetByID(currentUser.ID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to reload user", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}

	// Group items by aisle
	aisleGroups := make(map[string][]users.ShoppingListItem)
	for _, item := range currentUser.ShoppingList {
		aisle := item.AisleDescription
		if aisle == "" {
			aisle = "Other"
		}
		aisleGroups[aisle] = append(aisleGroups[aisle], item)
	}

	// Sort aisles for consistent display
	var aisles []string
	for aisle := range aisleGroups {
		aisles = append(aisles, aisle)
	}
	sort.Strings(aisles)

	// Get user's favorite location if available
	var location *locations.Location
	if currentUser.FavoriteStore != "" {
		location, err = s.locServer.GetLocationByID(ctx, currentUser.FavoriteStore)
		if err != nil {
			slog.WarnContext(ctx, "failed to load favorite store", "store_id", currentUser.FavoriteStore, "error", err)
		}
	}

	data := struct {
		ClarityScript template.HTML
		User          *users.User
		Location      *locations.Location
		Aisles        []string
		AisleGroups   map[string][]users.ShoppingListItem
		Style         seasons.Style
	}{
		ClarityScript: templates.ClarityScript(),
		User:          currentUser,
		Location:      location,
		Aisles:        aisles,
		AisleGroups:   aisleGroups,
		Style:         seasons.GetCurrentStyle(),
	}

	if err := templates.ShoppingList.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "shopping list template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
