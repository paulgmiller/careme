package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/parallelism"
	"careme/internal/recipes/feedback"

	"github.com/samber/lo"
)

const (
	recipeCachePrefix       = "recipe/"
	ShoppingListCachePrefix = "shoppinglist/"
	ingredientsCachePrefix  = "ingredients/"
	paramsCachePrefix       = "params/"
)

type recipeio struct {
	Cache               cache.Cache
	feedback.FeedbackIO // should this be pulled out?
}

func IO(c cache.Cache) recipeio {
	return recipeio{
		Cache:      c,
		FeedbackIO: feedback.NewIO(c),
	}
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
		return nil, fmt.Errorf("error getting shopping list for hash %s: %w", hash, err)
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
	// have to convert legacy hashes because each recipe stored an origin hash and we didn't rewrite them
	paramsReader, err := rio.Cache.Get(ctx, primaryKey)
	if err != nil {
		return nil, fmt.Errorf("error getting params for hash %s: %w", hash, err)
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
	// honor legacy hashes? I don't think so gets converted in server
	primaryKey := ingredientsCachePrefix + hash
	ingredientBlob, err := rio.Cache.Get(ctx, primaryKey)
	if err != nil {
		return nil, fmt.Errorf("error getting ingredients for hash %s: %w", hash, err)
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
	// Save each recipe separately by its hash (could skip ones that are saved?)
	_, err := parallelism.MapWithErrors(recipes, func(r ai.Recipe) (bool, error) {
		hash := r.ComputeHash()
		recipeJSON := lo.Must(json.Marshal(r))
		if err := rio.Cache.Put(ctx, recipeCachePrefix+hash, string(recipeJSON), cache.IfNoneMatch()); err != nil {
			if errors.Is(err, cache.ErrAlreadyExists) {
				return false, nil
			}
			slog.ErrorContext(ctx, "failed to cache individual recipe", "recipe", r.Title, "error", err)
			return false, err
		}
		slog.InfoContext(ctx, "stored recipe", "title", r.Title, "hash", hash)
		return true, nil
	})
	return err
}

var ErrAlreadyExists = errors.New("already exists")

func (rio recipeio) SaveParams(ctx context.Context, p *generatorParams) error {
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

func (rio recipeio) SaveShoppingList(ctx context.Context, shoppingList *ai.ShoppingList, hash string) error {
	for i := range shoppingList.Recipes {
		recipe := &shoppingList.Recipes[i]
		recipe.OriginHash = hash
	}
	for i := range shoppingList.Discarded {
		recipe := &shoppingList.Discarded[i]
		recipe.OriginHash = hash
	}

	// Save each recipe separately by its hash
	if err := rio.SaveRecipes(ctx, append(shoppingList.Recipes, shoppingList.Discarded...), hash); err != nil {
		return err
	}
	// we could actually nuke out the rest of recipe and lazily load but not yet
	shoppingList.Discarded = nil
	shoppingJSON := lo.Must(json.Marshal(shoppingList))
	if err := rio.Cache.Put(ctx, ShoppingListCachePrefix+hash, string(shoppingJSON), cache.Unconditional()); err != nil {
		slog.ErrorContext(ctx, "failed to cache shopping list document", "hash", hash, "error", err)
		return err
	}

	return nil
}
