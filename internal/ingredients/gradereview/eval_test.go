package gradereview

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var testReviewTime = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func seedReview(t *testing.T, c cache.Cache, key, location string, score int, verdict Verdict) {
	t.Helper()
	review := Review{GradeKey: key, LocationID: location, ReviewedAt: testReviewTime, Verdict: verdict, Ingredient: ai.InputIngredient{ProductID: "one", Brand: "Farm", Description: "Asparagus", Grade: &ai.IngredientGrade{Score: score, Reason: "Fresh"}, Embedding: ai.IngredientEmbedding{1, 0}}}
	body, err := json.Marshal(review)
	require.NoError(t, err)
	require.NoError(t, c.Put(t.Context(), reviewCachePrefix+key, string(body), cache.Unconditional()))
}

func TestExportReviewBounds(t *testing.T) {
	for _, tt := range []struct {
		name    string
		score   int
		verdict Verdict
		bounds  scoreBounds
	}{
		{"high", 8, VerdictTooHigh, scoreBounds{0, 8}},
		{"correct", 8, VerdictCorrect, scoreBounds{7, 9}},
		{"low", 8, VerdictTooLow, scoreBounds{8, 10}},
		{"high zero", 0, VerdictTooHigh, scoreBounds{0, 0}},
		{"correct zero", 0, VerdictCorrect, scoreBounds{0, 1}},
		{"correct ten", 10, VerdictCorrect, scoreBounds{9, 10}},
		{"low ten", 10, VerdictTooLow, scoreBounds{10, 10}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := cache.NewInMemoryCache()
			seedReview(t, c, "version/key", "a", tt.score, tt.verdict)
			var out bytes.Buffer
			require.NoError(t, WriteEvalCases(t.Context(), &out, c, EvalOptions{}))
			var tests []evalTest
			require.NoError(t, yaml.Unmarshal(out.Bytes(), &tests))
			require.Len(t, tests, 1)
			require.Len(t, tests[0].Vars.Cases, 1)
			assert.Equal(t, tt.bounds, tests[0].Vars.Cases[0].Expect)
			assert.Equal(t, "one", tests[0].Vars.Cases[0].Ingredient["id"])
			assert.NotContains(t, tests[0].Vars.Cases[0].Ingredient, "grade")
			assert.NotContains(t, tests[0].Vars.Cases[0].Ingredient, "embeddings")
			assert.Equal(t, "a", tests[0].Metadata["location_id"])
			assert.Equal(t, tt.score, tests[0].Metadata["reviewed_score"])
		})
	}
}

func TestExportFiltersAndDeterminism(t *testing.T) {
	c := cache.NewInMemoryCache()
	seedReview(t, c, "v2/z", "b", 5, VerdictTooLow)
	seedReview(t, c, "v1/a", "a", 9, VerdictCorrect)
	seedReview(t, c, "v1/b", "", 3, VerdictTooHigh) // legacy review
	for _, tt := range []struct {
		options EvalOptions
		count   int
	}{
		{EvalOptions{}, 3}, {EvalOptions{LocationID: "a"}, 1}, {EvalOptions{CacheVersion: "v1"}, 2}, {EvalOptions{CacheVersion: "v1", LocationID: "a"}, 1},
	} {
		var first, second bytes.Buffer
		require.NoError(t, WriteEvalCases(t.Context(), &first, c, tt.options))
		require.NoError(t, WriteEvalCases(t.Context(), &second, c, tt.options))
		assert.Equal(t, first.String(), second.String())
		var tests []evalTest
		require.NoError(t, yaml.Unmarshal(first.Bytes(), &tests))
		assert.Len(t, tests, tt.count)
		assert.Equal(t, "v1/a", tests[0].Metadata["grade_key"])
	}
	var out bytes.Buffer
	require.ErrorContains(t, WriteEvalCases(t.Context(), &out, c, EvalOptions{LocationID: "missing"}), "no ingredient reviews")
	assert.Empty(t, out.String())
}

func TestExportRejectsMalformedReviews(t *testing.T) {
	for _, body := range []string{
		"invalid json", `{}`, `{"grade_key":"v/key","verdict":"wrong"}`,
		`{"grade_key":"v/key","verdict":"correct","reviewed_at":"2026-10-08T12:00:00Z","ingredient":{"grade":{"score":11}}}`,
		`{"grade_key":"other/key","verdict":"correct","reviewed_at":"2026-10-08T12:00:00Z","ingredient":{"grade":{"score":5}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			c := cache.NewInMemoryCache()
			seedReview(t, c, "v/a", "", 3, VerdictCorrect)
			require.NoError(t, c.Put(t.Context(), reviewCachePrefix+"v/key", body, cache.Unconditional()))
			var out bytes.Buffer
			require.Error(t, WriteEvalCases(t.Context(), &out, c, EvalOptions{}))
			assert.Empty(t, out.String(), "no partial dataset")
		})
	}
	require.ErrorContains(t, WriteEvalCases(t.Context(), &bytes.Buffer{}, cache.NewInMemoryCache(), EvalOptions{}), "no ingredient reviews")
}

func TestExportWriterFailure(t *testing.T) {
	c := cache.NewInMemoryCache()
	seedReview(t, c, "v/key", "", 5, VerdictCorrect)
	require.ErrorContains(t, WriteEvalCases(t.Context(), &rejectWriter{}, c, EvalOptions{}), "encode ingredient eval cases")
}

type rejectWriter struct{}

func (*rejectWriter) Write([]byte) (int, error) { return 0, assert.AnError }
