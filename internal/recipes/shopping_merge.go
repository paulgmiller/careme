package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/parallelism"
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
	// Keep the first successful merge as the finalized list's snapshot.
	key := shoppingQuantityCachePrefix + hash
	exists, err := s.Cache.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("check merged shopping quantities: %w", err)
	}
	if exists {
		return nil
	}

	products, err := shoppingCatalogLookup(catalog)
	if err != nil {
		return err
	}

	merged, err := s.shoppingMerger.MergeShoppingQuantities(ctx, ingredients, MatchingShoppingCatalog(ingredients, catalog))
	if err != nil {
		return err
	}
	purchases := shoppingPurchasesForDisplay(prepareShoppingItems(merged, products))
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	wineCtx, stopWaiting := context.WithTimeout(ctx, 30*time.Second)
	defer stopWaiting()
	var catalog []ai.InputIngredient
	var catalogErr error
	var catalogWG sync.WaitGroup
	catalogWG.Go(func() {
		catalog, catalogErr = s.IngredientsFromCache(ctx, p.LocationHash())
		if errors.Is(catalogErr, cache.ErrNotFound) {
			catalogErr = nil
		}
		if catalogErr != nil {
			cancel()
		}
	})
	type wineInput struct {
		ingredients []ai.Ingredient
		catalog     []ai.InputIngredient
	}
	wines, wineErr := parallelism.MapWithErrors(p.Saved, func(recipe ai.Recipe) (wineInput, error) {
		selection, err := s.waitForWine(wineCtx, recipe.ComputeHash())
		// Wine is optional once its polling deadline expires; request cancellation is not.
		if errors.Is(err, context.DeadlineExceeded) && wineCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			slog.ErrorContext(ctx, "timed out waiting for wine pick; finalizing without wine for recipe", "recipe_hash", recipe.ComputeHash(), "recipe_title", recipe.Title, "error", err)
			return wineInput{}, nil
		}
		if err != nil {
			cancel()
			return wineInput{}, fmt.Errorf("finalize wine for recipe %q: %w", recipe.Title, err)
		}
		if len(selection.Wines) == 0 {
			return wineInput{}, nil
		}
		selectedCatalog, err := s.wineIngredientsFromCache(ctx, recipe.ComputeHash())
		// Older picks remain displayable even if their ordering metadata was never stored.
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			cancel()
			return wineInput{}, fmt.Errorf("load selected wine catalog: %w", err)
		}
		wine := selection.Wines[0]
		wine.Quantity = "1 bottle"
		return wineInput{ingredients: []ai.Ingredient{wine}, catalog: selectedCatalog}, nil
	})
	catalogWG.Wait()
	if catalogErr != nil {
		return nil, nil, fmt.Errorf("load shopping catalog: %w", catalogErr)
	}
	if wineErr != nil {
		return nil, nil, wineErr
	}
	ingredients := make([]ai.Ingredient, 0)
	for _, recipe := range p.Saved {
		ingredients = append(ingredients, recipe.Ingredients...)
	}
	for _, wine := range wines {
		ingredients = append(ingredients, wine.ingredients...)
		catalog = append(catalog, wine.catalog...)
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

func (s *server) finalizeShoppingPurchases(ctx context.Context, hash string) error {
	exists, err := s.Cache.Exists(ctx, shoppingQuantityCachePrefix+hash)
	if err != nil {
		return fmt.Errorf("check finalized shopping list: %w", err)
	}
	if exists {
		return nil
	}
	ingredients, catalog, err := s.finalizedShoppingInputs(ctx, hash)
	if err != nil {
		return err
	}
	return s.mergeAndCacheShoppingList(ctx, hash, ingredients, catalog)
}
