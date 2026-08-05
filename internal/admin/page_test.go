package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPageShowsBuildMetadata(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rr := httptest.NewRecorder()

	Page().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "<dt>Git hash</dt>")
	assert.Contains(t, rr.Body.String(), "<dt>Commit time</dt>")
	assert.Contains(t, rr.Body.String(), "<dt>Go version</dt>")
	assert.Contains(t, rr.Body.String(), "<dt>Dirty tree</dt>")
	assert.Contains(t, rr.Body.String(), `href="/admin/users"`)
}

func TestPageRejectsUnsupportedMethods(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/admin/", nil)
	rr := httptest.NewRecorder()

	Page().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}
