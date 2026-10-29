package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"careme/internal/ai"
	"careme/internal/cache"
)

const shoppingQuantityCachePrefix = "shopping_quantities/v7/"

type ShoppingQuantityMerger interface {
	MergeShoppingQuantities(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error)
}

// SimpleShoppingQuantityMerger passes ingredients through for the local mock server.
type SimpleShoppingQuantityMerger struct{}

func (SimpleShoppingQuantityMerger) MergeShoppingQuantities(_ context.Context, ingredients []ai.Ingredient, catalog []ai.InputIngredient) ([]ai.ShoppingItem, error) {
	result := make([]ai.ShoppingItem, 0, len(ingredients))
	for _, ingredient := range ingredients {
		result = append(result, ai.ShoppingItem{ProductID: ingredient.ProductID, Name: ingredient.Name, Needed: ingredient.Quantity})
	}
	return result, nil
}

func (s *server) mergeAndCacheShoppingList(ctx context.Context, hash string, ingredients []ai.Ingredient, catalog []ai.InputIngredient) error {
	catalog = ai.MatchingShoppingCatalog(ingredients, catalog)
	// Keep the first successful merge as the finalized list's snapshot.
	key := shoppingQuantityCachePrefix + hash
	exists, err := s.Cache.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("check merged shopping quantities: %w", err)
	}
	if exists {
		return nil
	}

	merged, err := s.shoppingMerger.MergeShoppingQuantities(ctx, ingredients, catalog)
	if err != nil {
		return err
	}
	purchases := shoppingPurchasesForDisplay(merged, catalog)
	body, err := json.Marshal(purchases)
	if err != nil {
		return err
	}
	if err := s.Cache.Put(ctx, key, string(body), cache.IfNoneMatch()); err != nil && !errors.Is(err, cache.ErrAlreadyExists) {
		return fmt.Errorf("cache merged shopping quantities: %w", err)
	}

	return nil
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
		ingredients = append(ingredients, recipe.Ingredients...)
	}
	catalog, err := s.IngredientsFromCache(ctx, p.LocationHash())
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return nil, nil, fmt.Errorf("load shopping catalog: %w", err)
	}
	return ingredients, catalog, nil
}

// Loading a page never invokes the model; Build creates this snapshot.
func (s *server) cachedShoppingPurchases(ctx context.Context, hash string) ([]shoppingPurchaseGroup, error) {
	reader, err := s.Cache.Get(ctx, shoppingQuantityCachePrefix+hash)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	var purchases []shoppingPurchaseGroup
	if err := json.NewDecoder(reader).Decode(&purchases); err != nil {
		return nil, fmt.Errorf("decode merged shopping quantities: %w", err)
	}
	return purchases, nil
}
