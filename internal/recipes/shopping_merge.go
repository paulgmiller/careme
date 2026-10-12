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

const shoppingQuantityCachePrefix = "shopping_quantities/v5/"

type ShoppingQuantityMerger interface {
	MergeShoppingQuantities(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.Ingredient, error)
}

// SimpleShoppingQuantityMerger is used by the local mock server.
type SimpleShoppingQuantityMerger struct{}

func (SimpleShoppingQuantityMerger) MergeShoppingQuantities(_ context.Context, ingredients []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.Ingredient, error) {
	result := make([]ai.Ingredient, 0)
	for _, group := range shoppingListForDisplay(ingredients) {
		for _, ingredient := range group.Items {
			result = append(result, *ingredient)
		}
	}
	return result, nil
}

func shoppingQuantityFingerprint(ingredients []ai.Ingredient, catalog []ai.InputIngredient) (string, error) {
	products := make([]ai.InputIngredient, len(catalog))
	for i, product := range catalog {
		product.Grade = nil
		product.Embedding = nil
		products[i] = product
	}
	ingredientBytes, err := json.Marshal(struct {
		Ingredients []ai.Ingredient      `json:"ingredients"`
		Catalog     []ai.InputIngredient `json:"catalog"`
	}{ingredients, products})
	if err != nil {
		return "", fmt.Errorf("marshal shopping ingredients: %w", err)
	}
	fingerprint := sha256.Sum256(ingredientBytes)
	return hex.EncodeToString(fingerprint[:]), nil
}

func (s *server) mergedShoppingList(ctx context.Context, hash string, ingredients []ai.Ingredient, catalog []ai.InputIngredient) ([]shoppingListGroup, error) {
	catalog = ai.MatchingShoppingCatalog(ingredients, catalog)
	fingerprint, err := shoppingQuantityFingerprint(ingredients, catalog)
	if err != nil {
		return nil, err
	}
	key := shoppingQuantityCachePrefix + hash + "/" + fingerprint
	var merged []ai.Ingredient
	reader, err := s.Cache.Get(ctx, key)
	switch {
	case err == nil:
		defer func() { _ = reader.Close() }()
		if err := json.NewDecoder(reader).Decode(&merged); err != nil {
			return nil, fmt.Errorf("decode merged shopping quantities: %w", err)
		}
	case errors.Is(err, cache.ErrNotFound):
		if s.shoppingMerger == nil {
			return nil, fmt.Errorf("shopping quantity merger unavailable")
		}
		merged, err = s.shoppingMerger.MergeShoppingQuantities(ctx, ingredients, catalog)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(merged)
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
			if err := json.NewDecoder(winner).Decode(&merged); err != nil {
				return nil, fmt.Errorf("decode concurrent shopping merge: %w", err)
			}
		}
	default:
		return nil, fmt.Errorf("load merged shopping quantities: %w", err)
	}
	products := make(map[string]ai.InputIngredient, len(catalog))
	for _, product := range catalog {
		products[product.ProductID] = product
	}
	for i := range merged {
		if product, ok := products[merged[i].ProductID]; ok {
			merged[i].Name = product.Description
			merged[i].AisleNumber = product.AisleNumber
		}
	}
	return shoppingPurchasesForDisplay(merged), nil
}

func (s *server) finalizedShoppingInputs(ctx context.Context, hash string) ([]ai.Ingredient, []ai.InputIngredient, error) {
	p, err := s.ParamsFromCache(ctx, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("load finalized shopping list: %w", err)
	}
	if len(p.Saved) == 0 {
		return nil, nil, fmt.Errorf("shopping list is not finalized")
	}
	ingredients := make([]ai.Ingredient, 0)
	for _, recipe := range p.Saved {
		wine, err := s.WineFromCache(ctx, recipe.ComputeHash())
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			return nil, nil, fmt.Errorf("load wine: %w", err)
		}
		ingredients = append(ingredients, ingredientsForDisplay(recipe.Ingredients, wine)...)
	}
	catalog, err := s.cachedShoppingCatalog(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	return ingredients, catalog, nil
}

func (s *server) handleShoppingQuantities(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimSpace(r.PathValue("hash"))
	ingredients, catalog, err := s.finalizedShoppingInputs(r.Context(), hash)
	if err != nil {
		http.Error(w, "Shopping list unavailable", http.StatusBadRequest)
		return
	}
	list, err := s.mergedShoppingList(r.Context(), hash, ingredients, catalog)
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

// Catalog snapshots are optional candidates. Missing snapshots leave needs unresolved.
func (s *server) cachedShoppingCatalog(ctx context.Context, p *generatorParams) ([]ai.InputIngredient, error) {
	keys := []string{p.LocationHash(), "pantry/query-categories-v1/" + p.LocationHash()}
	for _, recipe := range p.Saved {
		if styles := normalizedWineStyles(recipe.WineStyles); len(styles) > 0 {
			keys = append(keys, wineStylesCacheKey(styles, p.Location.ID, p.Date))
		}
	}
	products := make([]ai.InputIngredient, 0)
	seenKeys := make(map[string]bool)
	seenProducts := make(map[string]bool)
	for _, key := range keys {
		if seenKeys[key] {
			continue
		}
		seenKeys[key] = true
		candidates, err := s.IngredientsFromCache(ctx, key)
		if errors.Is(err, cache.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load shopping catalog %s: %w", key, err)
		}
		for _, product := range candidates {
			if product.ProductID == "" || seenProducts[product.ProductID] {
				continue
			}
			seenProducts[product.ProductID] = true
			product.Grade = nil
			product.Embedding = nil
			products = append(products, product)
		}
	}
	return products, nil
}
