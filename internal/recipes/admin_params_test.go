package recipes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminParamsJSON(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewFileCache(t.TempDir())
	params := DefaultParams(&locations.Location{
		ID:      "loc-123",
		Name:    "Test Store",
		ZipCode: "98101",
	}, time.Date(2026, time.May, 6, 0, 0, 0, 0, time.UTC))
	params.Instructions = "make it vegetarian"
	require.NoError(t, IO(cacheStore).SaveParams(t.Context(), params))

	mux := http.NewServeMux()
	mux.Handle("/params/{hash}", AdminParamsJSON(cacheStore))
	req := httptest.NewRequest(http.MethodGet, "/params/"+params.Hash(), nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, rr.Body.String(), "\n  \"location\":")
	assert.Contains(t, rr.Body.String(), "\n  \"instructions\": \"make it vegetarian\"")

	var got GeneratorParams
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.NotNil(t, got.Location)
	assert.Equal(t, "loc-123", got.Location.ID)
	assert.Equal(t, "make it vegetarian", got.Instructions)
}

func TestAdminParamsJSONMissingHash(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.Handle("/params/{hash}", AdminParamsJSON(cache.NewFileCache(t.TempDir())))
	req := httptest.NewRequest(http.MethodGet, "/params/missing", nil)
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "parameters not found in cache")
}
