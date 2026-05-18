package produce

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadScoresScansRecentCachedParamsAndUsesLatestPerLocation(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	now := time.Date(2026, time.May, 17, 12, 0, 0, 0, time.UTC)
	seedCachedIngredients(t, cacheStore, "12345", "Fresh Market", time.Date(2026, time.May, 15, 0, 0, 0, 0, time.UTC), []ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 8),
	})
	seedCachedIngredients(t, cacheStore, "12345", "Fresh Market", time.Date(2026, time.May, 16, 0, 0, 0, 0, time.UTC), []ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 8),
		ingredient("beet-1", "Fresh Beets", 8),
	})
	seedCachedIngredients(t, cacheStore, "67890", "Old Market", time.Date(2026, time.May, 9, 0, 0, 0, 0, time.UTC), []ai.InputIngredient{
		ingredient("parsnip-1", "Fresh Parsnips", 8),
	})

	scores, err := LoadScores(t.Context(), cacheStore, 7, now)

	require.NoError(t, err)
	require.Len(t, scores, 1)
	assert.Equal(t, "12345", scores[0].LocationID)
	assert.Equal(t, "2026-05-16", scores[0].Date)
	assert.Equal(t, 2, scores[0].ProduceScore.MatchedFamilies)
}

func TestAdminScoresPageRendersHTMLAndJSON(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	seedCachedIngredients(t, cacheStore, "12345", "Fresh Market", time.Now(), []ai.InputIngredient{
		ingredient("carrot-1", "Fresh Carrots", 8),
	})
	mux := http.NewServeMux()
	mux.Handle("/produce-scores", AdminScoresPage(cacheStore))

	htmlReq := httptest.NewRequest(http.MethodGet, "/produce-scores", nil)
	htmlRR := httptest.NewRecorder()
	mux.ServeHTTP(htmlRR, htmlReq)

	require.Equal(t, http.StatusOK, htmlRR.Code)
	assert.Contains(t, htmlRR.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, htmlRR.Body.String(), "Produce Scores")
	assert.Contains(t, htmlRR.Body.String(), "Fresh Market")

	jsonReq := httptest.NewRequest(http.MethodGet, "/produce-scores?format=json&days=7", nil)
	jsonRR := httptest.NewRecorder()
	mux.ServeHTTP(jsonRR, jsonReq)

	require.Equal(t, http.StatusOK, jsonRR.Code)
	assert.Contains(t, jsonRR.Header().Get("Content-Type"), "application/json")
	var body struct {
		Days   int             `json:"days"`
		Scores []LocationScore `json:"scores"`
	}
	require.NoError(t, json.Unmarshal(jsonRR.Body.Bytes(), &body))
	assert.Equal(t, 7, body.Days)
	require.Len(t, body.Scores, 1)
	assert.Equal(t, "12345", body.Scores[0].LocationID)
}

func TestAdminScoresPageRejectsInvalidDays(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.Handle("/produce-scores", AdminScoresPage(cache.NewFileCache(t.TempDir())))
	req := httptest.NewRequest(http.MethodGet, "/produce-scores?days=0", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "days must be between")
}

func TestLoadScoresSkipsUnsupportedCachedLocations(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	params := recipes.DefaultParams(&locations.Location{
		ID:      "unsupported-location",
		Name:    "Unsupported Market",
		ZipCode: "98101",
	}, time.Date(2026, time.May, 16, 0, 0, 0, 0, time.UTC))
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	require.NoError(t, cacheStore.Put(t.Context(), "params/unsupported", string(raw), cache.Unconditional()))

	scores, err := LoadScores(t.Context(), cacheStore, 7, time.Date(2026, time.May, 17, 12, 0, 0, 0, time.UTC))

	require.NoError(t, err)
	assert.Empty(t, scores)
}

func seedCachedIngredients(t *testing.T, cacheStore cache.Cache, locationID, name string, date time.Time, ingredients []ai.InputIngredient) {
	t.Helper()
	params := recipes.DefaultParams(&locations.Location{
		ID:      locationID,
		Name:    name,
		Chain:   "test",
		ZipCode: "98101",
	}, date)
	rio := recipes.IO(cacheStore)
	require.NoError(t, rio.SaveParams(t.Context(), params))
	require.NoError(t, rio.SaveIngredients(t.Context(), params.LocationHash(), ingredients))
}
