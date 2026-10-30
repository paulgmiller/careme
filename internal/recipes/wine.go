package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/parallelism"
)

const wineRecommendationsCachePrefix = "wine_recommendations/"

func recipeWineCacheKey(hash string) string {
	return wineRecommendationsCachePrefix + hash
}

func (rio recipeio) wineFromCache(ctx context.Context, hash string) (*ai.WineSelection, error) {
	wineReader, err := rio.Cache.Get(ctx, recipeWineCacheKey(hash))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = wineReader.Close()
	}()
	var selection ai.WineSelection
	err = json.NewDecoder(wineReader).Decode(&selection)
	return &selection, err
}

func (rio recipeio) wineExists(ctx context.Context, hash string) (bool, error) {
	return rio.Cache.Exists(ctx, recipeWineCacheKey(hash))
}

func (rio recipeio) saveWine(ctx context.Context, hash string, selection *ai.WineSelection) error {
	if selection == nil {
		return fmt.Errorf("wine selection is required")
	}
	body, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	return rio.Cache.Put(ctx, recipeWineCacheKey(hash), string(body), cache.Unconditional())
}

const selectedWineIngredientsPrefix = "wine_selection_ingredients/v1/"

func (rio recipeio) saveWineIngredients(ctx context.Context, hash string, ingredients []ai.InputIngredient) error {
	body, err := json.Marshal(ingredients)
	if err != nil {
		return fmt.Errorf("encode selected wine ingredients: %w", err)
	}
	return rio.Cache.Put(ctx, selectedWineIngredientsPrefix+hash, string(body), cache.Unconditional())
}

func (rio recipeio) wineIngredientsFromCache(ctx context.Context, hash string) ([]ai.InputIngredient, error) {
	reader, err := rio.Cache.Get(ctx, selectedWineIngredientsPrefix+hash)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	var ingredients []ai.InputIngredient
	if err := json.NewDecoder(reader).Decode(&ingredients); err != nil {
		return nil, fmt.Errorf("decode selected wine ingredients: %w", err)
	}
	return ingredients, nil
}

// wineCatalogForRecipes loads the snapshots used for shopping-item enrichment.
// Missing snapshots are allowed for legacy picks; unmatched products remain display-only.
func (rio recipeio) wineCatalogForRecipes(ctx context.Context, recipes []ai.Recipe) ([]ai.InputIngredient, error) {
	return parallelism.Flatten(recipes, func(recipe ai.Recipe) ([]ai.InputIngredient, error) {
		hash := recipe.ComputeHash()
		ingredients, err := rio.wineIngredientsFromCache(ctx, hash)
		if errors.Is(err, cache.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("load selected wine catalog for recipe %q: %w", hash, err)
		}
		return ingredients, nil
	})
}
