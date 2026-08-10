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
	jobID := recipeRegenerationJobID("old-hash", "response/id+with=padding")

	require.Len(t, jobID, 22)
	assert.False(t, strings.ContainsAny(jobID, "+/="))
	assert.Equal(t, jobID, recipeRegenerationJobID("old-hash", "response/id+with=padding"))
	assert.NotEqual(t, jobID, recipeRegenerationJobID("other-hash", "response/id+with=padding"))
}

func TestHandleSingleRecipeRegenerationRendersPersistedRunningJob(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	s := newTestServer(t, withTestCache(cacheStore))
	jobID := recipeRegenerationJobID("old-hash", "response-id")
	require.NoError(t, s.createRecipeRegenerationJob(t.Context(), jobID))

	path := "/recipe/old-hash/regen/" + jobID
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("hash", "old-hash")
	req.SetPathValue("jobID", jobID)
	rr := httptest.NewRecorder()

	s.handleSingleRecipeRegeneration(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), `hx-get="`+path+`"`)
	assert.False(t, strings.Contains(rr.Body.String(), "start="))
}
