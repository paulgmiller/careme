// Package embeddings caches ingredient vectors independently of ingredient grades.
package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
)

type Embedder interface {
	EmbedIngredients(context.Context, []string) ([]ai.IngredientEmbedding, error)
	CacheVersion() string
}

type Service struct {
	cache    cache.Cache
	embedder Embedder
}

func New(c cache.Cache, embedder Embedder) *Service {
	return &Service{cache: c, embedder: embedder}
}

func cacheKey(version, description string) string {
	hash := sha256.Sum256([]byte(description))
	return "ingredient_embeddings/" + version + "/" + hex.EncodeToString(hash[:])
}

// EmbedIngredients resolves vectors from the configured embedding cache even when
// the input carries vectors: ingredient snapshots do not identify their model.
func (s *Service) EmbedIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	result := append([]ai.InputIngredient(nil), ingredients...)
	byDescription := make(map[string][]int)
	descriptions := make([]string, 0, len(result))
	for i, ingredient := range result {
		description := strings.TrimSpace(ingredient.Description)
		if description == "" {
			return nil, fmt.Errorf("ingredient %q has an empty description", ingredient.ProductID)
		}
		if _, exists := byDescription[description]; !exists {
			descriptions = append(descriptions, description)
		}
		byDescription[description] = append(byDescription[description], i)
	}
	missing := make([]string, 0, len(descriptions))
	assign := func(description string, vector ai.IngredientEmbedding) {
		for _, i := range byDescription[description] {
			result[i].Embedding = vector
		}
	}
	for _, description := range descriptions {
		key := cacheKey(s.embedder.CacheVersion(), description)
		reader, err := s.cache.Get(ctx, key)
		if errors.Is(err, cache.ErrNotFound) {
			missing = append(missing, description)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load ingredient embedding %q: %w", description, err)
		}
		var vector ai.IngredientEmbedding
		err = json.NewDecoder(reader).Decode(&vector)
		_ = reader.Close()
		if err != nil {
			return nil, fmt.Errorf("decode ingredient embedding %q: %w", description, err)
		}
		assign(description, vector)
	}
	// Match the grading batch size while keeping embedding requests independent.
	for start := 0; start < len(missing); start += 30 {
		batch := missing[start:min(start+30, len(missing))]
		vectors, err := s.embedder.EmbedIngredients(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("embed ingredients: %w", err)
		}
		if len(vectors) != len(batch) {
			return nil, fmt.Errorf("expected %d embeddings, got %d", len(batch), len(vectors))
		}
		for i, description := range batch {
			body, err := json.Marshal(vectors[i])
			if err != nil {
				return nil, fmt.Errorf("encode ingredient embedding %q: %w", description, err)
			}
			if err := s.cache.Put(ctx, cacheKey(s.embedder.CacheVersion(), description), string(body), cache.Unconditional()); err != nil {
				return nil, fmt.Errorf("save ingredient embedding %q: %w", description, err)
			}
			assign(description, vectors[i])
		}
	}
	return result, nil
}
