package ai

import (
	"context"
	"fmt"
	"slices"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/tphakala/simd/f64"
)

const IngredientEmbeddingModel = openai.EmbeddingModelTextEmbedding3Small

type IngredientEmbedding []float64

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
		Dimensions:     openai.Int(256), //https://chatgpt.com/share/6aa863fa-5a50-83e8-8c88-177fe8d257c2
	})
	if err != nil {
		return nil, fmt.Errorf("create ingredient embeddings: %w", err)
	}
	if len(resp.Data) != len(descriptions) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(descriptions), len(resp.Data))
	}
	result := make([]IngredientEmbedding, len(descriptions))
	for _, item := range resp.Data {
		if item.Index < 0 || item.Index >= int64(len(result)) || result[item.Index] != nil {
			return nil, fmt.Errorf("invalid embedding index %d", item.Index)
		}
		result[item.Index] = IngredientEmbedding(item.Embedding)
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
	neighbors := make([]IngredientNeighbor, 0, len(ingredients))
	for _, ingredient := range ingredients {
		if ingredient.Embedding == nil {
			return nil, fmt.Errorf("ingredient %q has no embedding", ingredient.ProductID)
		}
		embedding := ingredient.Embedding
		similarity, err := cosineSimilarity(query, embedding)
		if err != nil {
			return nil, fmt.Errorf("ingredient %q: %w", ingredient.ProductID, err)
		}
		// Keep the CLI result readable without copying vectors into its output.
		ingredient.Embedding = nil
		neighbors = append(neighbors, IngredientNeighbor{Ingredient: ingredient, Similarity: similarity})
	}
	//replace with  containers heap
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

// because openai embeeddings are normalizerd we just need a dot product?
func cosineSimilarity(a, b []float64) (float64, error) {
	return f64.DotProduct(a, b), nil
}

/*	if len(a) == 0 || len(a) != len(b) {
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
}*/
