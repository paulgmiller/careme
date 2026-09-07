package recipes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/cache"
	"careme/internal/recipes/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSingleRecipeRegenerationRendersPersistedRunningJob(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	s := newTestServer(t, withTestCache(cacheStore))
	jobID := status.ID("old-hash", "response-id")
	require.NoError(t, s.generationStatuses.Start(t.Context(), jobID))

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

	jobID := status.ID("old-hash", "response-id")
	require.NoError(t, s.generationStatuses.Start(t.Context(), jobID))
	require.NoError(t, s.generationStatuses.Fail(t.Context(), jobID, fmt.Errorf("timed out i guess")))

	path := "/recipe/old-hash/regen/" + jobID
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("hash", "old-hash")
	req.SetPathValue("jobID", jobID)
	rr := httptest.NewRecorder()

	s.handleSingleRecipeRegeneration(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `/recipe/old-hash/regenerate`)
	assert.Contains(t, rr.Body.String(), "Try again, chef")
}

func TestHandleSingleRecipeRegenerationRejectsInvalidJobID(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	s := newTestServer(t, withTestCache(cacheStore))
	require.NoError(t, s.generationStatuses.Start(t.Context(), "not-an-id"))

	req := httptest.NewRequest(http.MethodGet, "/recipe/old-hash/regen/not-an-id", nil)
	req.SetPathValue("hash", "old-hash")
	req.SetPathValue("jobID", "not-an-id")
	rr := httptest.NewRecorder()

	s.handleSingleRecipeRegeneration(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "recipe regeneration not found")
}
