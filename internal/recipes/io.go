package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/samber/lo"
)

const recipeCachePrefix = "recipe/"

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
	defer recipe.Close()

	var singleRecipe ai.Recipe
	err = json.NewDecoder(recipe).Decode(&singleRecipe)
	if err != nil {
		return nil, err
	}
	return &singleRecipe, nil
}

func (rio recipeio) FromCache(ctx context.Context, hash string) (*ai.ShoppingList, error) {
	shoppinglist, err := rio.Cache.Get(ctx, hash)
	if err != nil {
		return nil, err
	}
	defer shoppinglist.Close()

	var list ai.ShoppingList
	err = json.NewDecoder(shoppinglist).Decode(&list)
	if err != nil {
		slog.ErrorContext(ctx, "failed to read cached recipe for hash", "hash", hash, "error", err)
		return nil, err
	}

	slog.InfoContext(ctx, "serving shared shoppingList by hash", "hash", hash)
	return &list, nil
}

// exported for backfilling
func (rio recipeio) SaveRecipes(ctx context.Context, recipes []ai.Recipe, originHash string) error {
	// Save each recipe separately by its hash
	var errs []error
	for i := range recipes {
		recipe := &recipes[i]
		recipe.OriginHash = originHash
		hash := recipe.ComputeHash()
		exists, err := rio.Cache.Exists(ctx, recipeCachePrefix+hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to check existing recipe in cache", "recipe", recipe.Title, "error", err)
			errs = append(errs, fmt.Errorf("error checking %s, %w", hash, err))
			continue
		}
		if exists {
			continue
		}

		slog.InfoContext(ctx, "storing recipe", "title", recipe.Title, "hash", hash)
		recipeJSON := lo.Must(json.Marshal(recipe))
		if err := rio.Cache.Set(ctx, recipeCachePrefix+hash, string(recipeJSON)); err != nil {
			slog.ErrorContext(ctx, "failed to cache individual recipe", "recipe", recipe.Title, "error", err)
			errs = append(errs, fmt.Errorf("error saving %s, %w", hash, err))
		}
	}
	return errors.Join(errs...)
}

var AlreadyExists = errors.New("already exists")

func (rio *recipeio) SaveParams(ctx context.Context, p *generatorParams) error {
	if exists, _ := rio.Cache.Exists(ctx, p.Hash()+".params"); exists {
		return AlreadyExists
	}

	paramsJSON := lo.Must(json.Marshal(p))
	if err := rio.Cache.Set(ctx, p.Hash()+".params", string(paramsJSON)); err != nil {
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
	if err := rio.Cache.Set(ctx, hash, string(shoppingJSON)); err != nil {
		slog.ErrorContext(ctx, "failed to cache shopping list document", "hash", hash, "error", err)
		return err
	}

	return nil
}
