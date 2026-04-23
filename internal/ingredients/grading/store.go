package grading

import (
	"context"
	"encoding/json"
	"fmt"

	"careme/internal/ai"
	"careme/internal/cache"
)

const cachePrefix = "ingredient_grades/"

func cacheKey(locationHash string, ingredientHash string) string {
	return cachePrefix + locationHash + "/" + ingredientHash
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

func (s store) Load(ctx context.Context, key string) (*ai.IngredientGrade, error) {
	reader, err := s.cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var grade ai.IngredientGrade
	if err := json.NewDecoder(reader).Decode(&grade); err != nil {
		return nil, err
	}
	return &grade, nil
}

func (s store) Save(ctx context.Context, key string, grade *ai.IngredientGrade) error {
	if grade == nil {
		return fmt.Errorf("ingredient grade is required")
	}
	body, err := json.Marshal(grade)
	if err != nil {
		return err
	}
	return s.cache.Put(ctx, key, string(body), cache.Unconditional())
}
