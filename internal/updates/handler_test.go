package updates

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServePublishedUpdates(t *testing.T) {
	store := cache.NewInMemoryCache()
	require.NoError(t, store.Put(context.Background(), FeedKey, "[]", cache.Unconditional()))
	require.NoError(t, store.Put(context.Background(), "params/private", "private", cache.Unconditional()))
	handler := Handler(store)
	for _, tc := range []struct {
		name, method, path              string
		status                          int
		body, contentType, cacheControl string
	}{
		{name: "feed", method: "GET", path: "/updates/feed.json", status: 200, body: "[]", contentType: "application/json", cacheControl: "public, max-age=300"},
		{name: "HEAD", method: "HEAD", path: "/updates/feed.json", status: 200, contentType: "application/json", cacheControl: "public, max-age=300"},
		{name: "no media proxy", method: "GET", path: "/updates/post/hash.mp4", status: 404},
		{name: "missing", method: "GET", path: "/updates/missing", status: 404},
		{name: "other data", method: "GET", path: "/params/private", status: 404},
		{name: "traversal", method: "GET", path: "/updates/../params/private", status: 404},
		{name: "no writes", method: "POST", path: "/updates/feed.json", status: 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			require.Equal(t, tc.status, response.Code)
			if tc.status < 400 {
				assert.Equal(t, tc.body, response.Body.String())
				assert.Equal(t, tc.contentType, response.Header().Get("Content-Type"))
				assert.Equal(t, tc.cacheControl, response.Header().Get("Cache-Control"))
			}
		})
	}
}

type brokenStore struct{ cache.Cache }

func (brokenStore) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("storage unavailable")
}

func TestStorageErrorsAreReported(t *testing.T) {
	handler := Handler(brokenStore{Cache: cache.NewInMemoryCache()})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/updates/feed.json", nil))
	assert.Equal(t, http.StatusInternalServerError, response.Code)
}
