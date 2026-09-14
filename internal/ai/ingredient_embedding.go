package ai

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/tphakala/simd/f64"
)

const IngredientEmbeddingModel = openai.EmbeddingModelTextEmbedding3Small

const IngredientEmbeddingDimensions = 256

type IngredientEmbedding []float64

type ingredientEmbedder struct{ oai openai.Client }

func NewIngredientEmbedder(apiKey string, httpClient *http.Client) *ingredientEmbedder {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	return &ingredientEmbedder{oai: openai.NewClient(opts...)}
}

func (g *ingredientEmbedder) CacheVersion() string {
	return fmt.Sprintf("%s/%d/description-v1", IngredientEmbeddingModel, IngredientEmbeddingDimensions)
}

// EmbedIngredients embeds descriptions, excluding prices, IDs, and grades.
func (g *ingredientEmbedder) EmbedIngredients(ctx context.Context, descriptions []string) ([]IngredientEmbedding, error) {
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
		Dimensions:     openai.Int(IngredientEmbeddingDimensions), // https://chatgpt.com/share/6aa863fa-5a50-83e8-8c88-177fe8d257c2
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
		if err := validateEmbedding(item.Embedding); err != nil {
			return nil, fmt.Errorf("embedding %d: %w", item.Index, err)
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
	if err := validateEmbedding(query); err != nil {
		return nil, fmt.Errorf("query embedding: %w", err)
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
	// replace with  containers heap
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

// OpenAI embeddings are normalized, so cosine similarity is their dot product.
func cosineSimilarity(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("embedding dimensions differ")
	}
	if err := validateEmbedding(b); err != nil {
		return 0, err
	}
	return f64.DotProduct(a, b), nil
}

func validateEmbedding(vector []float64) error {
	norm := f64.DotProduct(vector, vector)
	if len(vector) == 0 || norm == 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return fmt.Errorf("embedding is empty, zero, or non-finite")
	}
	return nil
}
