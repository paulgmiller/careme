package critique

import (
	"context"
	"encoding/json"
	"fmt"

	"careme/internal/ai"
	"careme/internal/cache"
)

const cachePrefix = "recipe_critiques/"

func cacheKey(hash string) string {
	return cachePrefix + hash
}

type store struct {
	cache cache.ListCache
}

func NewStore(c cache.ListCache) store {
	if c == nil {
		panic("cache must not be nil")
	}
	return store{cache: c}
}

func (s store) Load(ctx context.Context, hash string) (*ai.RecipeCritique, error) {
	critiqueReader, err := s.cache.Get(ctx, cacheKey(hash))
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

func (s store) Save(ctx context.Context, hash string, critique *ai.RecipeCritique) error {
	if critique == nil {
		return fmt.Errorf("recipe critique is required")
	}
	body, err := json.Marshal(critique)
	if err != nil {
		return err
	}
	return s.cache.Put(ctx, cacheKey(hash), string(body), cache.Unconditional())
}

func (s store) ListHashes(ctx context.Context) ([]string, error) {
	return s.cache.List(ctx, cachePrefix, "")
}
