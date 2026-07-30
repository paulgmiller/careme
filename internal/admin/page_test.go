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

	page(pageData{
		GitHash:   "0123456789abcdef",
		BuildTime: "2026-07-29T18:30:00Z",
	}).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "<code>0123456789abcdef</code>")
	assert.Contains(t, rr.Body.String(), "<time>2026-07-29T18:30:00Z</time>")
	assert.Contains(t, rr.Body.String(), `href="/admin/users"`)
}

func TestPageRejectsUnsupportedMethods(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/admin/", nil)
	rr := httptest.NewRecorder()

	page(pageData{}).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
}
