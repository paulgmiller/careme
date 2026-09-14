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
	"careme/internal/parallelism"
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
	type lookupResult struct {
		vector  ai.IngredientEmbedding
		missing bool
	}
	version := s.embedder.CacheVersion()
	lookups, err := parallelism.MapWithErrors(descriptions, func(description string) (lookupResult, error) {
		reader, err := s.cache.Get(ctx, cacheKey(version, description))
		if errors.Is(err, cache.ErrNotFound) {
			return lookupResult{missing: true}, nil
		}
		if err != nil {
			return lookupResult{}, fmt.Errorf("load ingredient embedding %q: %w", description, err)
		}
		defer func() { _ = reader.Close() }()
		var vector ai.IngredientEmbedding
		if err := json.NewDecoder(reader).Decode(&vector); err != nil {
			return lookupResult{}, fmt.Errorf("decode ingredient embedding %q: %w", description, err)
		}
		return lookupResult{vector: vector}, nil
	})
	if err != nil {
		return nil, err
	}
	for i, lookup := range lookups {
		if lookup.missing {
			missing = append(missing, descriptions[i])
			continue
		}
		assign(descriptions[i], lookup.vector)
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
