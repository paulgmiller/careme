package updates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestPostFragmentsWalkArchiveAndEscapeContent(t *testing.T) {
	store := cache.NewInMemoryCache()
	posts := []Post{
		{ID: "old", Text: "Older dinner", Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "new", Text: `<script>alert(1)</script> https://careme.cooking/recipe/example?a=1&b=2`, Date: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Media: []Media{{Src: "new/photo.jpg", Type: "image/jpeg", Alt: `A "photo"`}, {Src: "new/video.mp4", Type: "video/mp4"}}},
	}
	data, err := json.Marshal(posts)
	require.NoError(t, err)
	require.NoError(t, store.Put(context.Background(), Prefix+"feed.json", string(data), cache.Unconditional()))
	handler := Handler(store)
	get := func(url string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", url, nil))
		return response
	}
	newest := get("/updates/post")
	require.Equal(t, 200, newest.Code)
	body := newest.Body.String()
	assert.Equal(t, 1, strings.Count(body, "<article"))
	assert.NotContains(t, body, "Older dinner")
	assert.NotContains(t, body, "<script>")
	assert.Contains(t, body, "&lt;script&gt;")
	assert.Contains(t, body, `href="https://careme.cooking/recipe/example?a=1&amp;b=2"`)
	assert.Contains(t, body, `hx-get="/updates/post?id=old"`)
	assert.Contains(t, body, `hx-target="#careme-feed"`)
	assert.Contains(t, body, `hx-swap="outerHTML"`)
	assert.Contains(t, body, `src="/updates/new/photo.jpg"`)
	assert.Contains(t, body, `<video controls playsinline preload="metadata"`)
	assert.Contains(t, body, `type="video/mp4"`)
	assert.Contains(t, body, "Post 1 of 2")
	older := get("/updates/post?id=old")
	require.Equal(t, 200, older.Code)
	assert.Contains(t, older.Body.String(), "Older dinner")
	assert.Contains(t, older.Body.String(), `hx-get="/updates/post?id=new"`)
	assert.Contains(t, older.Body.String(), "Post 2 of 2")
	assert.Equal(t, 404, get("/updates/post?id=unknown").Code)
}

func TestEmptyArchiveFragment(t *testing.T) {
	response := httptest.NewRecorder()
	Handler(cache.NewInMemoryCache()).ServeHTTP(response, httptest.NewRequest("GET", "/updates/post", nil))
	assert.Equal(t, 200, response.Code)
	assert.Contains(t, response.Body.String(), "No Careme posts")
}
