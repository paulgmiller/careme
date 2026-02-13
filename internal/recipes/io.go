package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/samber/lo"
)

const recipeCachePrefix = "recipe/"
const ShoppingListCachePrefix = "shoppinglist/"
const ingredientsCachePrefix = "ingredients/"
const paramsCachePrefix = "params/"

type recipeio struct {
	Cache cache.Cache
}

func IO(c cache.Cache) *recipeio {
	return &recipeio{c}
}

func (rio recipeio) SingleFromCache(ctx context.Context, hash string) (*ai.Recipe, error) {
	recipe, err := rio.Cache.Get(ctx, recipeCachePrefix+hash)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := recipe.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached recipe", "hash", hash, "error", err)
		}
	}()

	var singleRecipe ai.Recipe
	err = json.NewDecoder(recipe).Decode(&singleRecipe)
	if err != nil {
		return nil, err
	}
	return &singleRecipe, nil
}

func (rio recipeio) FromCache(ctx context.Context, hash string) (*ai.ShoppingList, error) {
	primaryKey := ShoppingListCachePrefix + hash
	shoppinglist, err := rio.Cache.Get(ctx, primaryKey)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			return nil, err
		}
		legacyHash, ok := legacyRecipeHash(hash)
		if !ok {
			return nil, err
		}
		shoppinglist, err = rio.Cache.Get(ctx, legacyHash)
		if err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "serving legacy cached shoppingList by hash", "hash", hash, "cache_key", legacyHash)
	}
	defer func() {
		if err := shoppinglist.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached shopping list", "hash", hash, "error", err)
		}
	}()

	var list ai.ShoppingList
	err = json.NewDecoder(shoppinglist).Decode(&list)
	if err != nil {
		slog.ErrorContext(ctx, "failed to read cached recipe for hash", "hash", hash, "error", err)
		return nil, err
	}

	slog.InfoContext(ctx, "serving shared shoppingList by hash", "hash", hash)
	return &list, nil
}

func (rio recipeio) ParamsFromCache(ctx context.Context, hash string) (*generatorParams, error) {
	primaryKey := paramsCachePrefix + hash
	paramsReader, err := rio.Cache.Get(ctx, primaryKey)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			return nil, fmt.Errorf("params not found for hash %s: %w", hash, err)
		}
		legacyHash, ok := legacyRecipeHash(hash)
		if !ok {
			return nil, fmt.Errorf("params not found for hash %s: %w", hash, err)
		}
		legacyKey := legacyHash + ".params"
		paramsReader, err = rio.Cache.Get(ctx, legacyKey)
		if err != nil {
			return nil, fmt.Errorf("params not found for hash %s: %w", hash, err)
		}
		slog.InfoContext(ctx, "serving legacy cached params by hash", "hash", hash, "cache_key", legacyKey)
	}
	defer func() {
		if err := paramsReader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close params reader", "hash", hash, "error", err)
		}
	}()

	var params generatorParams
	if err := json.NewDecoder(paramsReader).Decode(&params); err != nil {
		return nil, fmt.Errorf("failed to decode params: %w", err)
	}
	return &params, nil
}

func (rio recipeio) IngredientsFromCache(ctx context.Context, hash string) ([]kroger.Ingredient, error) {
	primaryKey := ingredientsCachePrefix + hash
	ingredientBlob, err := rio.Cache.Get(ctx, primaryKey)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			return nil, err
		}
		legacyHash, ok := legacyLocationHash(hash)
		if !ok {
			return nil, err
		}
		ingredientBlob, err = rio.Cache.Get(ctx, legacyHash)
		if err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "serving legacy cached ingredients by hash", "hash", hash, "cache_key", legacyHash)
	}
	defer func() {
		if err := ingredientBlob.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached ingredients reader", "hash", hash, "error", err)
		}
	}()

	var ingredients []kroger.Ingredient
	if err := json.NewDecoder(ingredientBlob).Decode(&ingredients); err != nil {
		return nil, err
	}
	return ingredients, nil
}

func (rio recipeio) SaveIngredients(ctx context.Context, hash string, ingredients []kroger.Ingredient) error {
	ingredientsJSON, err := json.Marshal(ingredients)
	if err != nil {
		return err
	}
	return rio.Cache.Put(ctx, ingredientsCachePrefix+hash, string(ingredientsJSON), cache.Unconditional())
}

// exported for backfilling
func (rio recipeio) SaveRecipes(ctx context.Context, recipes []ai.Recipe, originHash string) error {
	// Save each recipe separately by its hash
	var errs []error
	for i := range recipes {
		recipe := &recipes[i]
		recipe.OriginHash = originHash
		hash := recipe.ComputeHash()

		slog.InfoContext(ctx, "storing recipe", "title", recipe.Title, "hash", hash)
		recipeJSON := lo.Must(json.Marshal(recipe))
		if err := rio.Cache.Put(ctx, recipeCachePrefix+hash, string(recipeJSON), cache.IfNoneMatch()); err != nil {
			if errors.Is(err, cache.ErrAlreadyExists) {
				continue
			}
			slog.ErrorContext(ctx, "failed to cache individual recipe", "recipe", recipe.Title, "error", err)
			errs = append(errs, fmt.Errorf("error saving %s, %w", hash, err))
		}
	}
	return errors.Join(errs...)
}

var ErrAlreadyExists = errors.New("already exists")

func (rio *recipeio) SaveParams(ctx context.Context, p *generatorParams) error {
	paramsJSON := lo.Must(json.Marshal(p))
	if err := rio.Cache.Put(ctx, paramsCachePrefix+p.Hash(), string(paramsJSON), cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			return ErrAlreadyExists
		}
		slog.ErrorContext(ctx, "failed to cache params", "location", p.String(), "error", err)
		return err
	}
	return nil
}

func (rio *recipeio) SaveShoppingList(ctx context.Context, shoppingList *ai.ShoppingList, hash string) error {
	// Save each recipe separately by its hash
	if err := rio.SaveRecipes(ctx, shoppingList.Recipes, hash); err != nil {
		return err
	}
	// we could actually nuke out the rest of recipe and lazily load but not yet
	shoppingJSON := lo.Must(json.Marshal(shoppingList))
	if err := rio.Cache.Put(ctx, ShoppingListCachePrefix+hash, string(shoppingJSON), cache.Unconditional()); err != nil {
		slog.ErrorContext(ctx, "failed to cache shopping list document", "hash", hash, "error", err)
		return err
	}

	return nil
}
