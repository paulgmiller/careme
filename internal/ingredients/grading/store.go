package grading

import (
	"context"
	"encoding/json"
	"fmt"

	"careme/internal/ai"
	"careme/internal/cache"
)

const cachePrefix = "ingredient_grades/"

func cacheKey(ingredientHash string) string {
	return cachePrefix + ingredientHash
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

func (s store) Load(ctx context.Context, key string) (*ai.InputIngredient, error) {
	reader, err := s.cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var ingredient ai.InputIngredient
	if err := json.NewDecoder(reader).Decode(&ingredient); err != nil {
		return nil, err
	}
	return &ingredient, nil
}

func (s store) Save(ctx context.Context, key string, ingredient *ai.InputIngredient) error {
	if ingredient == nil {
		return fmt.Errorf("graded ingredient is required")
	}
	body, err := json.Marshal(ingredient)
	if err != nil {
		return err
	}
	return s.cache.Put(ctx, key, string(body), cache.Unconditional())
}
