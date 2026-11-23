package main

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

func readStored(t *testing.T, store cache.Cache, key string) []byte {
	t.Helper()
	r, err := store.Get(context.Background(), key)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return data
}

func TestExportPublishedPostsAndLocalMedia(t *testing.T) {
	store := cache.NewInMemoryCache()
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/graphql":
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Contains(t, body["query"], "first: 100")
			assert.Contains(t, body["query"], `organizationId: "org"`)
			assert.Contains(t, body["query"], `channelIds: ["facebook"]`)
			assert.Contains(t, body["query"], "status: [sent]")
			assert.NotContains(t, body["query"], "sort:")
			assert.NotContains(t, body["query"], "account")
			_, _ = io.WriteString(w, `{"data":{"posts":{"edges":[{"node":{"id":"post1","text":"Rice cakes!\nhttps://careme.cooking/recipe/example","dueAt":"2026-11-20T12:00:00Z","assets":[{"source":"`+endpoint+`/photo","mimeType":"image/jpeg","image":{"altText":"Dinner [photo]"}},{"source":"`+endpoint+`/video","mimeType":"video/mp4"}]}}]}}}`)
		case "/photo", "/video":
			assert.Empty(t, r.Header.Get("Authorization"), "Never send the Buffer key to media hosts")
			_, _ = io.WriteString(w, "media-bytes:"+r.URL.Path)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	endpoint = server.URL
	e := exporter{client: server.Client(), endpoint: server.URL + "/graphql", key: "test-key", store: store}
	require.NoError(t, e.run(context.Background(), "org", "facebook", 4))
	var posts []post
	require.NoError(t, json.Unmarshal(readStored(t, store, "updates/feed.json"), &posts))
	require.Len(t, posts, 1)
	assert.Equal(t, "post1", posts[0].ID)
	assert.Equal(t, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), posts[0].Date)
	require.Len(t, posts[0].Media, 2)
	assert.Equal(t, "image/jpeg", posts[0].Media[0].Type)
	assert.True(t, strings.HasSuffix(posts[0].Media[0].Src, ".jpg"))
	assert.Equal(t, "video/mp4", posts[0].Media[1].Type)
	assert.True(t, strings.HasSuffix(posts[0].Media[1].Src, ".mp4"))
	assert.Equal(t, "media-bytes:/photo", string(readStored(t, store, "updates/"+posts[0].Media[0].Src)))
	assert.Equal(t, "media-bytes:/video", string(readStored(t, store, "updates/"+posts[0].Media[1].Src)))
	markdown := string(readStored(t, store, "updates/post1/index.md"))
	var frontmatter struct {
		Title  string
		Params struct {
			BufferID string `json:"buffer_id"`
			Media    []media
		}
	}
	require.NoError(t, json.NewDecoder(strings.NewReader(markdown)).Decode(&frontmatter))
	assert.Equal(t, "Rice cakes!", frontmatter.Title)
	assert.Equal(t, "post1", frontmatter.Params.BufferID)
	assert.NotContains(t, frontmatter.Params.Media[0].Src, "/")
	assert.Contains(t, markdown, "[Watch video]")
	assert.NotContains(t, markdown, server.URL)
	assert.NotContains(t, string(readStored(t, store, "updates/feed.json")), "test-key")
}

func TestBufferErrorsPreservePriorFeed(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		want string
	}{
		{"HTTP", 403, "forbidden", "HTTP 403"},
		{"scope", 200, `{"errors":[{"message":"Insufficient scope"}]}`, "Insufficient scope"},
		{"malformed", 200, `{`, "decode Buffer posts"},
		{"empty", 200, `{"data":{"posts":{"edges":[]}}}`, "no published posts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := cache.NewInMemoryCache()
			require.NoError(t, store.Put(context.Background(), "updates/feed.json", "prior", cache.Unconditional()))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			e := exporter{client: server.Client(), endpoint: server.URL, key: "test-key", store: store}
			require.ErrorContains(t, e.run(context.Background(), "org", "channel", 4), tc.want)
			assert.Equal(t, "prior", string(readStored(t, store, "updates/feed.json")))
		})
	}
}

type failedUpload struct{ cache.Cache }

func (f failedUpload) PutReader(context.Context, string, io.Reader, cache.PutOptions) error {
	return errors.New("upload failed")
}

func TestMediaFailuresPreservePriorFeed(t *testing.T) {
	for _, tc := range []struct {
		name         string
		id           string
		mime         string
		failDownload bool
		failUpload   bool
		want         string
	}{
		{name: "download", id: "post1", mime: "video/mp4", failDownload: true, want: "download media: HTTP 404"},
		{name: "upload", id: "post1", mime: "video/mp4", failUpload: true, want: "upload failed"},
		{name: "unsupported", id: "post1", mime: "application/pdf", want: "unsupported media"},
		{name: "invalid ID", id: "../private", mime: "video/mp4", want: "invalid Buffer post ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := cache.NewInMemoryCache()
			require.NoError(t, store.Put(context.Background(), "updates/feed.json", "prior", cache.Unconditional()))
			var endpoint string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/media" {
					if tc.failDownload {
						http.NotFound(w, r)
						return
					}
					_, _ = io.WriteString(w, "video")
					return
				}
				p := bufferPost{ID: tc.id, Text: "Dinner", DueAt: time.Now(), Assets: []asset{{Source: endpoint + "/media", MimeType: tc.mime}}}
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"posts": map[string]any{"edges": []any{map[string]any{"node": p}}}}}))
			}))
			defer server.Close()
			endpoint = server.URL
			var target cache.Cache = store
			if tc.failUpload {
				target = failedUpload{Cache: store}
			}
			e := exporter{client: server.Client(), endpoint: server.URL, key: "test-key", store: target}
			require.ErrorContains(t, e.run(context.Background(), "org", "channel", 4), tc.want)
			assert.Equal(t, "prior", string(readStored(t, store, "updates/feed.json")))
		})
	}
}

func TestFetchPagesSelectsNewestAndDeduplicates(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		if requests == 1 {
			_, _ = io.WriteString(w, `{"data":{"posts":{"pageInfo":{"hasNextPage":true,"endCursor":"next"},"edges":[{"node":{"id":"older","dueAt":"2026-11-01T12:00:00Z"}}]}}}`)
		} else {
			assert.Contains(t, body["query"], `after: "next"`)
			_, _ = io.WriteString(w, `{"data":{"posts":{"pageInfo":{"hasNextPage":false},"edges":[{"node":{"id":"newer","dueAt":"2026-11-21T12:00:00Z"}},{"node":{"id":"older","dueAt":"2026-11-01T12:00:00Z"}}]}}}`)
		}
	}))
	defer server.Close()
	e := exporter{client: server.Client(), endpoint: server.URL, key: "test-key", store: cache.NewInMemoryCache()}
	posts, err := e.fetch(context.Background(), "org", "channel", 1)
	require.NoError(t, err)
	require.Len(t, posts, 1)
	assert.Equal(t, "newer", posts[0].ID)
	assert.Equal(t, 2, requests)
}
