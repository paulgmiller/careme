// Command bufferexport snapshots published Buffer posts to Azure-backed storage.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"careme/internal/cache"
	"careme/internal/httpretry"
	"careme/internal/updates"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/paulgmiller/kage/pkg/kage"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type asset struct {
	Source   string `json:"source"`
	MimeType string `json:"mimeType"`
	Image    struct {
		AltText string `json:"altText"`
	} `json:"image"`
}

type bufferPost struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	DueAt     time.Time `json:"dueAt"`
	CreatedAt time.Time `json:"createdAt"`
	Assets    []asset   `json:"assets"`
}

type exporter struct {
	client   *http.Client
	endpoint string
	key      string
	store    cache.Cache
}

func main() {
	organization := flag.String("organization-id", "69a67ba9800155d3c2ee9e08", "Buffer organization ID")
	channel := flag.String("channel-id", "69c40b8daf47dacb6953dd1a", "Careme Facebook channel ID")
	limit := flag.Int("limit", 4, "Refresh the newest N existing posts; 0 refreshes all (new posts are always imported)")
	secretFile := flag.String("secret-file", "", "Optional kage encrypted secret file")
	flag.Parse()
	if *limit < 0 {
		log.Fatal("limit must be nonnegative")
	}
	var err error
	if *secretFile == "" {
		err = kage.Load()
	} else {
		err = kage.Load(*secretFile)
	}
	if err != nil {
		log.Fatal(err)
	}
	key := os.Getenv("BUFFER_API_KEY")
	if key == "" {
		log.Fatal("configure buffer/BUFFER_API_KEY in kage or BUFFER_API_KEY in the environment")
	}
	if os.Getenv("AZURE_STORAGE_ACCOUNT_NAME") == "" {
		log.Fatal("Azure storage credentials are required")
	}
	store, err := cache.MakeCache()
	if err != nil {
		log.Fatal(err)
	}
	retry := retryablehttp.NewClient()
	retry.Logger = nil
	retry.RetryMax = 2
	retry.RequestLogHook = httpretry.LogRetry("buffer-export")
	retry.HTTPClient.Transport = otelhttp.NewTransport(http.DefaultTransport)
	retry.HTTPClient.Timeout = time.Minute
	e := exporter{client: retry.StandardClient(), endpoint: "https://api.buffer.com", key: key, store: store}
	if err := e.run(context.Background(), *organization, *channel, *limit); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Published the merged archive to updates/feed.json.")
}

type postPage struct {
	Edges []struct {
		Node bufferPost `json:"node"`
	} `json:"edges"`
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
}

func (p bufferPost) date() time.Time {
	if p.DueAt.IsZero() {
		return p.CreatedAt
	}
	return p.DueAt
}

func (e exporter) fetch(ctx context.Context, organization, channel string) ([]bufferPost, error) {
	// The public API does not expose the website's sort argument. Read all pages,
	// then select by publication date rather than depending on unspecified ordering.
	byID := map[string]bufferPost{}
	cursor := ""
	for {
		after := "null"
		if cursor != "" {
			after = fmt.Sprintf("%q", cursor)
		}
		query := fmt.Sprintf(`{ posts(first: 100, after: %s, input: {organizationId: %q, filter: {status: [sent], channelIds: [%q]}}) { pageInfo { hasNextPage endCursor } edges { node { id text dueAt createdAt assets { source mimeType ... on ImageAsset { image { altText } } } } } } }`, after, organization, channel)
		page, err := e.fetchPage(ctx, query)
		if err != nil {
			return nil, err
		}
		for _, edge := range page.Edges {
			byID[edge.Node.ID] = edge.Node
		}
		if !page.PageInfo.HasNextPage {
			break
		}
		if page.PageInfo.EndCursor == "" || page.PageInfo.EndCursor == cursor {
			return nil, errors.New("buffer returned a non-advancing pagination cursor")
		}
		cursor = page.PageInfo.EndCursor
	}
	posts := make([]bufferPost, 0, len(byID))
	for _, p := range byID {
		posts = append(posts, p)
	}
	sort.Slice(posts, func(i, j int) bool {
		if posts[i].date().Equal(posts[j].date()) {
			return posts[i].ID > posts[j].ID
		}
		return posts[i].date().After(posts[j].date())
	})
	if len(posts) == 0 {
		return nil, errors.New("no published posts returned; prior feed left unchanged")
	}
	return posts, nil
}

func (e exporter) fetchPage(ctx context.Context, query string) (postPage, error) {
	var page postPage
	body, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return page, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return page, err
	}
	req.Header.Set("Authorization", "Bearer "+e.key)
	req.Header.Set("Content-Type", "application/json")
	response, err := e.client.Do(req)
	if err != nil {
		return page, fmt.Errorf("read Buffer posts: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return page, fmt.Errorf("read Buffer posts: HTTP %d", response.StatusCode)
	}
	var result struct {
		Data struct {
			Posts postPage `json:"posts"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return page, fmt.Errorf("decode Buffer posts: %w", err)
	}
	if len(result.Errors) > 0 {
		return page, fmt.Errorf("read Buffer posts: %s", result.Errors[0].Message)
	}
	return result.Data.Posts, nil
}

func (e exporter) run(ctx context.Context, organization, channel string, limit int) error {
	// Buffer does not guarantee ordering: read metadata, but only sync media for
	// new posts and the newest limit existing posts. Zero explicitly refreshes all.
	source, err := e.fetch(ctx, organization, channel)
	if err != nil {
		return err
	}
	previous, err := updates.ReadPosts(ctx, e.store)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return fmt.Errorf("read prior archive: %w", err)
	}
	byID := make(map[string]updates.Post, len(previous))
	for _, p := range previous {
		byID[p.ID] = p
	}
	for index, original := range source {
		if !updates.ValidID(original.ID) {
			return fmt.Errorf("invalid Buffer post ID %q", original.ID)
		}
		if _, exists := byID[original.ID]; exists && limit > 0 && index >= limit {
			continue
		}
		exported := updates.Post{ID: original.ID, Text: original.Text, Date: original.date(), Media: []updates.Media{}}
		for _, item := range original.Assets {
			if !strings.HasPrefix(item.MimeType, "image/") && !strings.HasPrefix(item.MimeType, "video/") {
				return fmt.Errorf("post %s: unsupported media type %s", original.ID, item.MimeType)
			}
			src, err := e.copyMedia(ctx, original.ID, item)
			if err != nil {
				return fmt.Errorf("post %s: %w", original.ID, err)
			}
			exported.Media = append(exported.Media, updates.Media{Src: src, Type: item.MimeType, Alt: item.Image.AltText})
		}
		byID[original.ID] = exported
	}
	posts := make([]updates.Post, 0, len(byID))
	for _, p := range byID {
		posts = append(posts, p)
	}
	updates.SortPosts(posts)
	manifest, err := json.MarshalIndent(posts, "", "  ")
	if err != nil {
		return err
	}
	// Immutable media may be orphaned on failure; readers retain the last good
	// manifest until every required asset has been stored successfully.
	if err := e.store.Put(ctx, updates.Prefix+"feed.json", string(manifest), cache.Unconditional()); err != nil {
		return fmt.Errorf("publish feed: %w", err)
	}
	return nil
}

func (e exporter) copyMedia(ctx context.Context, id string, item asset) (string, error) {
	extension, err := extensionFor(item.MimeType)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.Source, nil)
	if err != nil {
		return "", err
	}
	// The Buffer bearer token is deliberately absent from media requests.
	response, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download media: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download media: HTTP %d", response.StatusCode)
	}
	file, err := os.CreateTemp("", "buffer-media-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hash), response.Body); err != nil {
		return "", fmt.Errorf("download media: %w", err)
	}
	relative := fmt.Sprintf("%s/%x%s", id, hash.Sum(nil)[:12], extension)
	key := updates.Prefix + relative
	exists, err := e.store.Exists(ctx, key)
	if err != nil {
		return "", err
	}
	if !exists {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		if err := e.store.PutReader(ctx, key, file, cache.Unconditional()); err != nil {
			return "", fmt.Errorf("upload %s: %w", key, err)
		}
	}
	return relative, nil
}

func extensionFor(contentType string) (string, error) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", nil
	case "video/mp4":
		return ".mp4", nil
	}
	extensions, err := mime.ExtensionsByType(contentType)
	if err != nil {
		return "", err
	}
	if len(extensions) == 0 {
		return "", fmt.Errorf("unknown media type %s", contentType)
	}
	return extensions[0], nil
}
