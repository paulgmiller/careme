// Command bufferexport snapshots published Buffer posts to Azure-backed storage.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
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
	secretFile := flag.String("secret-file", "", "Optional kage encrypted secret file")
	flag.Parse()
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
	if err := e.run(context.Background(), *organization, *channel); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Published the latest ten posts to updates/feed.json.")
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
	return posts[:min(10, len(posts))], nil
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

func (e exporter) run(ctx context.Context, organization, channel string) error {
	source, err := e.fetch(ctx, organization, channel)
	if err != nil {
		return err
	}
	posts := make([]updates.Post, 0, len(source))
	for _, original := range source {
		exported := updates.Post{ID: original.ID, Text: original.Text, Date: original.date(), Media: []updates.Media{}}
		for _, item := range original.Assets {
			if !strings.HasPrefix(item.MimeType, "image/") && !strings.HasPrefix(item.MimeType, "video/") {
				return fmt.Errorf("post %s: unsupported media type %s", original.ID, item.MimeType)
			}
			exported.Media = append(exported.Media, updates.Media{Src: item.Source, Type: item.MimeType, Alt: item.Image.AltText})
		}
		posts = append(posts, exported)
	}
	manifest, err := json.MarshalIndent(posts, "", "  ")
	if err != nil {
		return err
	}
	// Replace the feed only after the complete Buffer response has been processed.
	if err := e.store.Put(ctx, updates.FeedKey, string(manifest), cache.Unconditional()); err != nil {
		return fmt.Errorf("publish feed: %w", err)
	}
	return nil
}
