package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testEmbeddingResponse(req *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestEmbedIngredientsAPI(t *testing.T) {
	g := NewIngredientGrader("test", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "/v1/embeddings", req.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		assert.Equal(t, "text-embedding-3-small", body["model"])
		assert.Equal(t, "float", body["encoding_format"])
		assert.Equal(t, []any{"broccoli", "asparagus"}, body["input"])
		return testEmbeddingResponse(req, `{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`), nil
	})})
	got, err := g.EmbedIngredients(t.Context(), []string{"broccoli", "asparagus"})
	require.NoError(t, err)
	assert.Equal(t, []float64{1, 0}, got[0].Vector)
	assert.Equal(t, []float64{0, 1}, got[1].Vector)
	assert.Equal(t, string(IngredientEmbeddingModel), got[0].Model)
}

func TestEmbedIngredientsRejectsInvalidResponses(t *testing.T) {
	for name, body := range map[string]string{
		"missing": `{"data":[]}`,
		"empty":   `{"data":[{"index":0,"embedding":[]}]}`,
		"zero":    `{"data":[{"index":0,"embedding":[0,0]}]}`,
		"index":   `{"data":[{"index":2,"embedding":[1,0]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			g := NewIngredientGrader("test", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) { return testEmbeddingResponse(req, body), nil })})
			got, err := g.EmbedIngredients(t.Context(), []string{"broccoli"})
			require.Error(t, err)
			assert.Nil(t, got)
		})
	}
}

func TestGradeIngredientsFailsWhenEmbeddingFails(t *testing.T) {
	g := NewIngredientGrader("test", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/embeddings") {
			r := testEmbeddingResponse(req, `{"error":{"message":"bad input"}}`)
			r.StatusCode = 400
			return r, nil
		}
		return ingredientGradeHTTPResponse(req, `{"grades":[{"id":"a","score":9,"reason":"Fresh."}]}`), nil
	})})
	got, err := g.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "a", Description: "broccoli"}})
	require.ErrorContains(t, err, "embed graded ingredients")
	assert.Nil(t, got)
}

func TestNearestIngredients(t *testing.T) {
	query := IngredientEmbedding{Model: string(IngredientEmbeddingModel), Vector: []float64{1, 0}}
	item := func(id string, vector []float64) InputIngredient {
		return InputIngredient{ProductID: id, Grade: &IngredientGrade{Score: 8, Embedding: &IngredientEmbedding{Model: query.Model, Vector: vector}}}
	}
	catalog := []InputIngredient{item("far", []float64{-1, 0}), item("near", []float64{2, 1}), item("exact", []float64{4, 0})}
	got, err := NearestIngredients(query, catalog, 2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "exact", got[0].Ingredient.ProductID)
	assert.Equal(t, "near", got[1].Ingredient.ProductID)
	assert.InDelta(t, 1, got[0].Similarity, 1e-10)
	assert.InDelta(t, 2.0/2.2360679775, got[1].Similarity, 1e-10)
	assert.Nil(t, got[0].Ingredient.Grade.Embedding)
	assert.NotNil(t, catalog[2].Grade.Embedding)
	for name, ingredients := range map[string][]InputIngredient{
		"missing":    {{ProductID: "a"}},
		"dimensions": {item("a", []float64{1})},
		"zero":       {item("a", []float64{0, 0})},
		"model":      {{Grade: &IngredientGrade{Embedding: &IngredientEmbedding{Model: "other", Vector: []float64{1, 0}}}}},
	} {
		t.Run(name, func(t *testing.T) { _, err := NearestIngredients(query, ingredients, 1); require.Error(t, err) })
	}
}

func TestEmbedIngredientsRejectsDuplicateIndices(t *testing.T) {
	g := NewIngredientGrader("test", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return testEmbeddingResponse(req, `{"data":[{"index":0,"embedding":[1,0]},{"index":0,"embedding":[0,1]}]}`), nil
	})})
	_, err := g.EmbedIngredients(t.Context(), []string{"broccoli", "asparagus"})
	require.ErrorContains(t, err, "invalid embedding index")
}

func TestEmbedIngredientsEmptyInputs(t *testing.T) {
	g := NewIngredientGrader("test", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("unexpected API request")
		return nil, nil
	})})
	got, err := g.EmbedIngredients(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	_, err = g.EmbedIngredients(t.Context(), []string{" "})
	require.ErrorContains(t, err, "is empty")
}

func TestNearestIngredientsLimitsAndTies(t *testing.T) {
	query := IngredientEmbedding{Model: string(IngredientEmbeddingModel), Vector: []float64{1, 0}}
	catalog := []InputIngredient{
		{ProductID: "b", Grade: &IngredientGrade{Embedding: &query}},
		{ProductID: "a", Grade: &IngredientGrade{Embedding: &query}},
	}
	got, err := NearestIngredients(query, catalog, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Ingredient.ProductID)
	_, err = NearestIngredients(query, catalog, 0)
	require.Error(t, err)
	_, err = NearestIngredients(IngredientEmbedding{}, catalog, 1)
	require.Error(t, err)
	got, err = NearestIngredients(query, nil, 1)
	require.NoError(t, err)
	assert.Empty(t, got)
}
