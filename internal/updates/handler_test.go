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
	require.NoError(t, store.Put(context.Background(), Prefix+"feed.json", "[]", cache.Unconditional()))
	require.NoError(t, store.Put(context.Background(), Prefix+"post/hash.mp4", "0123456789", cache.Unconditional()))
	require.NoError(t, store.Put(context.Background(), Prefix+"post/hash.jpg", "photo", cache.Unconditional()))
	require.NoError(t, store.Put(context.Background(), "params/private", "private", cache.Unconditional()))
	handler := Handler(store)
	for _, tc := range []struct {
		name, method, path, rangeHeader string
		status                          int
		body, contentType, cacheControl string
	}{
		{"feed", "GET", "/updates/feed.json", "", 200, "[]", "application/json", "public, max-age=300"},
		{"photo", "GET", "/updates/post/hash.jpg", "", 200, "photo", "image/jpeg", "public, max-age=31536000, immutable"},
		{"video range", "GET", "/updates/post/hash.mp4", "bytes=2-4", 206, "234", "video/mp4", "public, max-age=31536000, immutable"},
		{"HEAD", "HEAD", "/updates/post/hash.mp4", "", 200, "", "video/mp4", "public, max-age=31536000, immutable"},
		{name: "missing", method: "GET", path: "/updates/missing", status: 404},
		{name: "other data", method: "GET", path: "/params/private", status: 404},
		{name: "traversal", method: "GET", path: "/updates/../params/private", status: 404},
		{name: "no writes", method: "POST", path: "/updates/feed.json", status: 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Range", tc.rangeHeader)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			require.Equal(t, tc.status, response.Code)
			if tc.status < 400 {
				assert.Equal(t, tc.body, response.Body.String())
				assert.Equal(t, tc.contentType, response.Header().Get("Content-Type"))
				assert.Equal(t, tc.cacheControl, response.Header().Get("Cache-Control"))
			}
			if tc.rangeHeader != "" {
				assert.Equal(t, "bytes 2-4/10", response.Header().Get("Content-Range"))
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
