package recipes

import (
	"context"
	"encoding/json"
	"fmt"

	"careme/internal/ai"
	"careme/internal/cache"
)

const recipeCritiquesCachePrefix = "recipe_critiques/"

func recipeCritiqueCacheKey(hash string) string {
	return recipeCritiquesCachePrefix + hash
}

func (rio recipeio) CritiqueFromCache(ctx context.Context, hash string) (*ai.RecipeCritique, error) {
	critiqueReader, err := rio.Cache.Get(ctx, recipeCritiqueCacheKey(hash))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = critiqueReader.Close()
	}()
	var critique ai.RecipeCritique
	err = json.NewDecoder(critiqueReader).Decode(&critique)
	return &critique, err
}

func (rio recipeio) SaveCritique(ctx context.Context, hash string, critique *ai.RecipeCritique) error {
	if critique == nil {
		return fmt.Errorf("recipe critique is required")
	}
	body, err := json.Marshal(critique)
	if err != nil {
		return err
	}
	return rio.Cache.Put(ctx, recipeCritiqueCacheKey(hash), string(body), cache.Unconditional())
}
