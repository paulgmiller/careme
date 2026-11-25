package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/updates"

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

func TestExportPublishedPostsAndBufferMediaURLs(t *testing.T) {
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
			t.Error("exporter must not request media URLs")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	endpoint = server.URL
	e := exporter{client: server.Client(), endpoint: server.URL + "/graphql", key: "test-key", store: store}
	require.NoError(t, e.run(context.Background(), "org", "facebook"))
	var posts []updates.Post
	require.NoError(t, json.Unmarshal(readStored(t, store, "updates/feed.json"), &posts))
	require.Len(t, posts, 1)
	assert.Equal(t, "post1", posts[0].ID)
	assert.Equal(t, time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), posts[0].Date)
	require.Len(t, posts[0].Media, 2)
	assert.Equal(t, "image/jpeg", posts[0].Media[0].Type)
	assert.Equal(t, endpoint+"/photo", posts[0].Media[0].Src)
	assert.Equal(t, "Dinner [photo]", posts[0].Media[0].Alt)
	assert.Equal(t, "video/mp4", posts[0].Media[1].Type)
	assert.Equal(t, endpoint+"/video", posts[0].Media[1].Src)
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
			require.NoError(t, store.Put(context.Background(), "updates/feed.json", "[]", cache.Unconditional()))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			e := exporter{client: server.Client(), endpoint: server.URL, key: "test-key", store: store}
			require.ErrorContains(t, e.run(context.Background(), "org", "channel"), tc.want)
			assert.Equal(t, "[]", string(readStored(t, store, "updates/feed.json")))
		})
	}
}

type failedPublish struct{ cache.Cache }

func (f failedPublish) Put(context.Context, string, string, cache.PutOptions) error {
	return errors.New("publish failed")
}

func TestExportFailuresPreservePriorFeed(t *testing.T) {
	for _, tc := range []struct {
		name, mime, want string
		failPublish      bool
	}{
		{name: "unsupported", mime: "application/pdf", want: "unsupported media"},
		{name: "publish", mime: "video/mp4", want: "publish failed", failPublish: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := cache.NewInMemoryCache()
			require.NoError(t, store.Put(context.Background(), "updates/feed.json", "prior", cache.Unconditional()))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p := bufferPost{ID: "post1", Text: "Dinner", DueAt: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), Assets: []asset{{Source: "https://media.example/video", MimeType: tc.mime}}}
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"posts": map[string]any{"edges": []any{map[string]any{"node": p}}}}}))
			}))
			defer server.Close()
			var target cache.Cache = store
			if tc.failPublish {
				target = failedPublish{Cache: store}
			}
			e := exporter{client: server.Client(), endpoint: server.URL, store: target}
			require.ErrorContains(t, e.run(context.Background(), "org", "channel"), tc.want)
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
	posts, err := e.fetch(context.Background(), "org", "channel")
	require.NoError(t, err)
	require.Len(t, posts, 2)
	assert.Equal(t, "newer", posts[0].ID)
	assert.Equal(t, 2, requests)
}

func TestRollingFeedReplacesPriorPostsWithLatestTen(t *testing.T) {
	ctx := context.Background()
	store := cache.NewInMemoryCache()
	// The exporter no longer reads or merges the previous manifest.
	require.NoError(t, store.Put(ctx, "updates/feed.json", "old snapshot", cache.Unconditional()))
	require.NoError(t, store.Put(ctx, "updates/old/photo.jpg", "keep old media", cache.Unconditional()))
	var source []bufferPost
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/media/") {
			t.Error("exporter must not request media URLs")
			return
		}
		edges := []any{}
		for _, p := range source {
			edges = append(edges, map[string]any{"node": p})
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"posts": map[string]any{"edges": edges}}}))
	}))
	defer server.Close()
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("p%d", i)
		source = append(source, bufferPost{ID: id, Text: "original", DueAt: time.Date(2026, 11, i+1, 0, 0, 0, 0, time.UTC), Assets: []asset{{Source: server.URL + "/media/" + id, MimeType: "image/jpeg"}}})
	}
	e := exporter{client: server.Client(), endpoint: server.URL, store: store}
	require.NoError(t, e.run(ctx, "org", "channel"))
	var posts []updates.Post
	require.NoError(t, json.Unmarshal(readStored(t, store, "updates/feed.json"), &posts))
	require.Len(t, posts, 10)
	assert.Equal(t, "p11", posts[0].ID)
	assert.Equal(t, "p2", posts[9].ID)
	assert.Equal(t, "keep old media", string(readStored(t, store, "updates/old/photo.jpg")))
	// Upstream deletions disappear from the feed; edits are reflected on every run.
	source = source[8:11]
	source[2].Text = "edited"
	require.NoError(t, e.run(ctx, "org", "channel"))
	require.NoError(t, json.Unmarshal(readStored(t, store, "updates/feed.json"), &posts))
	require.Len(t, posts, 3)
	assert.Equal(t, "p10", posts[0].ID)
	assert.Equal(t, "edited", posts[0].Text)
}
