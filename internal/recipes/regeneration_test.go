package recipes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/cache"
	"careme/internal/recipes/regeneration"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSingleRecipeRegenerationRendersPersistedRunningJob(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	s := newTestServer(t, withTestCache(cacheStore))
	jobID := regeneration.ID("old-hash", "response-id")
	require.NoError(t, s.regenerations.Start(t.Context(), jobID, cache.IfNoneMatch()))

	path := "/recipe/old-hash/regen/" + jobID
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("hash", "old-hash")
	req.SetPathValue("jobID", jobID)
	rr := httptest.NewRecorder()

	s.handleSingleRecipeRegeneration(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `hx-get="`+path+`"`)
	assert.NotContains(t, rr.Body.String(), "start=")
}

func TestHandleSingleRecipeRegenerationRendersRetryAfterTimeout(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	s := newTestServer(t, withTestCache(cacheStore))
	s.regenerations = regeneration.TimeoutStore(cacheStore)

	jobID := regeneration.ID("old-hash", "response-id")
	require.NoError(t, s.regenerations.Start(t.Context(), jobID, cache.IfNoneMatch()))

	path := "/recipe/old-hash/regen/" + jobID
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("hash", "old-hash")
	req.SetPathValue("jobID", jobID)
	rr := httptest.NewRecorder()

	s.handleSingleRecipeRegeneration(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), path+"/retry")
	assert.Contains(t, rr.Body.String(), "Try again, chef")
}
