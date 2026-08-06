package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminCritiqueEvalsPageShowsDatasetAndCritiqueDetails(t *testing.T) {
	t.Parallel()
	c := cache.NewInMemoryCache()
	dataset := adminCritiqueEvalDataset{
		ID:   "dataset-id",
		Name: "cooked-2026-08-06",
		Samples: []adminCritiqueEvalSample{{
			Hash:              "recipe-hash",
			Title:             "Sausage Orecchiette",
			FeedbackUpdatedAt: time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC),
			Stars:             4,
			Historical:        &ai.RecipeCritique{OverallScore: 9, Model: "gemini-old", Summary: "Historical summary."},
		}},
	}
	putAdminEvalJSON(t, c, critiqueEvalDatasetPrefix+dataset.Name+".json", dataset)
	putAdminEvalJSON(t, c, critiqueEvalResultPrefix+dataset.ID+"/fingerprint/model/recipe-hash.json", adminCritiqueEvalResult{
		PromptFingerprint: "fingerprint",
		RequestedModel:    "anthropic/claude-opus-5",
		RecipeHash:        "recipe-hash",
		Critique: &ai.RecipeCritique{
			OverallScore: 6,
			Model:        "anthropic/claude-opus-5",
			Summary:      "Fresh summary.",
			Issues:       []ai.RecipeCritiqueIssue{{Category: "timing", Severity: "high", Detail: "Timing is too short."}},
		},
	})

	handler := adminCritiqueEvalsPage(c)
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/critique-evals?dataset=cooked-2026-08-06&prompt=fingerprint", nil))
	require.Equal(t, http.StatusOK, list.Code)
	assert.Contains(t, list.Body.String(), "Sausage Orecchiette")
	assert.Contains(t, list.Body.String(), "6/fail/anthropic/claude-opus-5")

	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/critique-evals?dataset=cooked-2026-08-06&prompt=fingerprint&hash=recipe-hash", nil))
	require.Equal(t, http.StatusOK, detail.Code)
	assert.Contains(t, detail.Body.String(), "Historical summary.")
	assert.Contains(t, detail.Body.String(), "Fresh summary.")
	assert.Contains(t, detail.Body.String(), "[timing/high] Timing is too short.")
}

func TestAdminCritiqueEvalsPageRejectsPost(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	adminCritiqueEvalsPage(cache.NewInMemoryCache()).ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/critique-evals", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}

func putAdminEvalJSON(t *testing.T, c cache.Cache, key string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, c.Put(t.Context(), key, string(body), cache.Unconditional()))
}
