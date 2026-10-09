package recipes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/templates"
)

const shoppingQuantityCachePrefix = "shopping_quantities/v1/"

type ShoppingQuantityMerger interface {
	MergeShoppingQuantities(context.Context, []ai.ShoppingQuantityGroup) (map[string]string, error)
}

// SimpleShoppingQuantityMerger is used by the local mock server.
type SimpleShoppingQuantityMerger struct{}

func (SimpleShoppingQuantityMerger) MergeShoppingQuantities(_ context.Context, groups []ai.ShoppingQuantityGroup) (map[string]string, error) {
	result := make(map[string]string, len(groups))
	for _, group := range groups {
		quantity := ""
		for _, amount := range group.Quantities {
			quantity = mergeShoppingQuantities(quantity, amount)
		}
		result[group.Key] = quantity
	}
	return result, nil
}

func shoppingQuantityGroups(ingredients []ai.Ingredient) []ai.ShoppingQuantityGroup {
	groups := make([]ai.ShoppingQuantityGroup, 0)
	indexes := make(map[string]int)
	for _, item := range ingredients {
		key := shoppingListKey(item)
		if normalizeShoppingListName(item.Name) == "" {
			continue
		}
		index, found := indexes[key]
		if !found {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, ai.ShoppingQuantityGroup{Key: key, Name: item.Name})
		}
		groups[index].Quantities = append(groups[index].Quantities, strings.TrimSpace(item.Quantity))
	}
	return groups
}

func shoppingQuantityFingerprint(ingredients []ai.Ingredient) (string, error) {
	groupBytes, err := json.Marshal(shoppingQuantityGroups(ingredients))
	if err != nil {
		return "", fmt.Errorf("marshal shopping quantity groups: %w", err)
	}
	fingerprint := sha256.Sum256(groupBytes)
	return hex.EncodeToString(fingerprint[:]), nil
}

func (s *server) mergedShoppingList(ctx context.Context, hash string, ingredients []ai.Ingredient) ([]shoppingListGroup, error) {
	quantityGroups := shoppingQuantityGroups(ingredients)
	fingerprint, err := shoppingQuantityFingerprint(ingredients)
	if err != nil {
		return nil, err
	}
	key := shoppingQuantityCachePrefix + hash + "/" + fingerprint
	var amounts map[string]string
	reader, err := s.Cache.Get(ctx, key)
	switch {
	case err == nil:
		defer func() { _ = reader.Close() }()
		if err := json.NewDecoder(reader).Decode(&amounts); err != nil {
			return nil, fmt.Errorf("decode merged shopping quantities: %w", err)
		}
	case errors.Is(err, cache.ErrNotFound):
		if s.shoppingMerger == nil {
			return nil, fmt.Errorf("shopping quantity merger unavailable")
		}
		amounts, err = s.shoppingMerger.MergeShoppingQuantities(ctx, quantityGroups)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(amounts)
		if err != nil {
			return nil, err
		}
		if err := s.Cache.Put(ctx, key, string(body), cache.IfNoneMatch()); err != nil {
			if !errors.Is(err, cache.ErrAlreadyExists) {
				return nil, fmt.Errorf("cache merged shopping quantities: %w", err)
			}
			winner, err := s.Cache.Get(ctx, key)
			if err != nil {
				return nil, fmt.Errorf("load concurrent shopping merge: %w", err)
			}
			defer func() { _ = winner.Close() }()
			if err := json.NewDecoder(winner).Decode(&amounts); err != nil {
				return nil, fmt.Errorf("decode concurrent shopping merge: %w", err)
			}
		}
	default:
		return nil, fmt.Errorf("load merged shopping quantities: %w", err)
	}
	list := shoppingListForDisplay(ingredients)
	for _, group := range list {
		for _, item := range group.Items {
			quantity, ok := amounts[shoppingListKey(*item)]
			if !ok {
				return nil, fmt.Errorf("missing merged shopping quantity for %q", item.Name)
			}
			item.Quantity = quantity
		}
	}
	return list, nil
}

func (s *server) finalizedShoppingIngredients(ctx context.Context, hash string) ([]ai.Ingredient, error) {
	p, err := s.ParamsFromCache(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("load finalized shopping list: %w", err)
	}
	if len(p.Saved) == 0 {
		return nil, fmt.Errorf("shopping list is not finalized")
	}
	ingredients := make([]ai.Ingredient, 0)
	for _, recipe := range p.Saved {
		wine, err := s.WineFromCache(ctx, recipe.ComputeHash())
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			return nil, fmt.Errorf("load wine: %w", err)
		}
		ingredients = append(ingredients, ingredientsForDisplay(recipe.Ingredients, wine)...)
	}
	return ingredients, nil
}

func (s *server) handleShoppingQuantities(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimSpace(r.PathValue("hash"))
	ingredients, err := s.finalizedShoppingIngredients(r.Context(), hash)
	if err != nil {
		http.Error(w, "Shopping list unavailable", http.StatusBadRequest)
		return
	}
	list, err := s.mergedShoppingList(r.Context(), hash, ingredients)
	view := shoppingListPageView{Hash: hash, ShoppingList: list, HasSavedRecipes: true}
	name := "shopping_list_section"
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to merge shopping quantities", "hash", hash, "error", err)
		name = "shopping_quantity_error"
	}
	if err := templates.ShoppingList.ExecuteTemplate(w, name, view); err != nil {
		slog.ErrorContext(r.Context(), "render shopping quantities", "error", err)
	}
}
