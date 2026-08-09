package recipes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"careme/internal/cache"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecipeRegenerationJobIDIsStableAndURLSafe(t *testing.T) {
	jobID := recipeRegenerationJobID("old-hash", "response/id+with=padding", 1)

	require.Len(t, jobID, 22)
	assert.False(t, strings.ContainsAny(jobID, "+/="))
	assert.Equal(t, jobID, recipeRegenerationJobID("old-hash", "response/id+with=padding", 1))
	assert.NotEqual(t, jobID, recipeRegenerationJobID("other-hash", "response/id+with=padding", 1))
	assert.NotEqual(t, jobID, recipeRegenerationJobID("old-hash", "response/id+with=padding", 2))
}

func TestHandleSingleRecipeRegenerationRendersPersistedRunningJob(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	s := newTestServer(t, withTestCache(cacheStore))
	job := newRecipeRegenerationJob("old-hash", "response-id")
	created, err := s.createRecipeRegenerationJob(t.Context(), job)
	require.NoError(t, err)
	require.True(t, created)

	path := "/recipe/old-hash/regen/" + job.ID
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("hash", job.OldHash)
	req.SetPathValue("jobID", job.ID)
	rr := httptest.NewRecorder()

	s.handleSingleRecipeRegeneration(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `hx-get="`+path+`"`)
	assert.False(t, strings.Contains(rr.Body.String(), "start="))
}
