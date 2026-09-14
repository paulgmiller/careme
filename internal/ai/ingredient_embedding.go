package ai

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	openai "github.com/openai/openai-go/v3"
)

const IngredientEmbeddingModel = openai.EmbeddingModelTextEmbedding3Small

type IngredientEmbedding struct {
	Model  string    `json:"model"`
	Vector []float64 `json:"vector"`
}

// EmbedIngredients embeds descriptions, excluding prices, IDs, and grades.
func (g *ingredientGrader) EmbedIngredients(ctx context.Context, descriptions []string) ([]IngredientEmbedding, error) {
	if len(descriptions) == 0 {
		return nil, nil
	}
	for i, description := range descriptions {
		if strings.TrimSpace(description) == "" {
			return nil, fmt.Errorf("ingredient description %d is empty", i)
		}
	}
	resp, err := g.oai.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model:          IngredientEmbeddingModel,
		Input:          openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: descriptions},
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
	})
	if err != nil {
		return nil, fmt.Errorf("create ingredient embeddings: %w", err)
	}
	if len(resp.Data) != len(descriptions) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(descriptions), len(resp.Data))
	}
	result := make([]IngredientEmbedding, len(descriptions))
	for _, item := range resp.Data {
		if item.Index < 0 || item.Index >= int64(len(result)) || result[item.Index].Vector != nil {
			return nil, fmt.Errorf("invalid embedding index %d", item.Index)
		}
		if _, err := cosineSimilarity(item.Embedding, item.Embedding); err != nil {
			return nil, fmt.Errorf("embedding %d: %w", item.Index, err)
		}
		result[item.Index] = IngredientEmbedding{Model: string(IngredientEmbeddingModel), Vector: item.Embedding}
	}
	return result, nil
}

type IngredientNeighbor struct {
	Ingredient InputIngredient `json:"ingredient"`
	Similarity float64         `json:"similarity"`
}

// NearestIngredients ranks a store's graded catalog by cosine similarity.
func NearestIngredients(query IngredientEmbedding, ingredients []InputIngredient, limit int) ([]IngredientNeighbor, error) {
	if limit < 1 {
		return nil, fmt.Errorf("neighbor limit must be positive")
	}
	if _, err := cosineSimilarity(query.Vector, query.Vector); err != nil {
		return nil, fmt.Errorf("query embedding: %w", err)
	}
	neighbors := make([]IngredientNeighbor, 0, len(ingredients))
	for _, ingredient := range ingredients {
		if ingredient.Grade == nil || ingredient.Grade.Embedding == nil {
			return nil, fmt.Errorf("ingredient %q has no embedding", ingredient.ProductID)
		}
		embedding := ingredient.Grade.Embedding
		if embedding.Model != query.Model {
			return nil, fmt.Errorf("ingredient %q embedding model mismatch", ingredient.ProductID)
		}
		similarity, err := cosineSimilarity(query.Vector, embedding.Vector)
		if err != nil {
			return nil, fmt.Errorf("ingredient %q: %w", ingredient.ProductID, err)
		}
		// Keep the CLI result readable without copying vectors into its output.
		grade := *ingredient.Grade
		grade.Embedding = nil
		ingredient.Grade = &grade
		neighbors = append(neighbors, IngredientNeighbor{Ingredient: ingredient, Similarity: similarity})
	}
	slices.SortFunc(neighbors, func(a, b IngredientNeighbor) int {
		if a.Similarity > b.Similarity {
			return -1
		}
		if a.Similarity < b.Similarity {
			return 1
		}
		return strings.Compare(a.Ingredient.ProductID, b.Ingredient.ProductID)
	})
	return neighbors[:min(limit, len(neighbors))], nil
}

func cosineSimilarity(a, b []float64) (float64, error) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, fmt.Errorf("embedding dimensions differ or are empty")
	}
	var dot, aa, bb float64
	for i, value := range a {
		dot += value * b[i]
		aa += value * value
		bb += b[i] * b[i]
	}
	if aa == 0 || bb == 0 || math.IsNaN(aa+bb+dot) || math.IsInf(aa+bb+dot, 0) {
		return 0, fmt.Errorf("embedding contains invalid values or has zero norm")
	}
	return dot / (math.Sqrt(aa) * math.Sqrt(bb)), nil
}
