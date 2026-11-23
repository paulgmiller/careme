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
	"path"
	"regexp"
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

type media struct {
	Src  string `json:"src"`
	Type string `json:"type"`
	Alt  string `json:"alt"`
}

type post struct {
	ID    string    `json:"id"`
	Text  string    `json:"text"`
	Date  time.Time `json:"date"`
	Media []media   `json:"media"`
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
	limit := flag.Int("limit", 4, "Number of published posts in the carousel")
	output := flag.String("output", "", "Write bundles to this directory instead of Azure (local testing/Hugo)")
	secretFile := flag.String("secret-file", "", "Optional kage encrypted secret file")
	flag.Parse()
	if *limit < 1 {
		log.Fatal("limit must be positive")
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
	var store cache.Cache
	if *output != "" {
		store = cache.NewFileCache(*output)
	} else {
		if os.Getenv("AZURE_STORAGE_ACCOUNT_NAME") == "" {
			log.Fatal("Azure storage credentials are required; use -output for a local export")
		}
		store, err = cache.MakeCache()
		if err != nil {
			log.Fatal(err)
		}
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
	fmt.Printf("Exported up to %d published posts. The carousel uses /updates/feed.json.\n", *limit)
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

func (e exporter) fetch(ctx context.Context, organization, channel string, limit int) ([]bufferPost, error) {
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
	if len(posts) > limit {
		posts = posts[:limit]
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

type upload struct {
	key  string
	data []byte
}

var validID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func (e exporter) run(ctx context.Context, organization, channel string, limit int) error {
	source, err := e.fetch(ctx, organization, channel, limit)
	if err != nil {
		return err
	}
	var uploads []upload
	posts := make([]post, 0, len(source))
	for _, original := range source {
		if !validID.MatchString(original.ID) {
			return fmt.Errorf("invalid Buffer post ID %q", original.ID)
		}
		date := original.date()
		exported := post{ID: original.ID, Text: original.Text, Date: date, Media: []media{}}
		markdown := original.Text + "\n"
		var bundleMedia []media
		for _, item := range original.Assets {
			if !strings.HasPrefix(item.MimeType, "image/") && !strings.HasPrefix(item.MimeType, "video/") {
				return fmt.Errorf("post %s: unsupported media type %s", original.ID, item.MimeType)
			}
			data, err := e.download(ctx, item.Source)
			if err != nil {
				return fmt.Errorf("post %s: %w", original.ID, err)
			}
			extension, err := extensionFor(item.MimeType)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(data)
			filename := fmt.Sprintf("%x%s", hash[:12], extension)
			relative := path.Join(original.ID, filename)
			uploads = append(uploads, upload{key: updates.Prefix + relative, data: data})
			exported.Media = append(exported.Media, media{Src: relative, Type: item.MimeType, Alt: item.Image.AltText})
			bundleMedia = append(bundleMedia, media{Src: filename, Type: item.MimeType, Alt: item.Image.AltText})
			if strings.HasPrefix(item.MimeType, "image/") {
				alt := strings.NewReplacer("[", `\[`, "]", `\]`).Replace(item.Image.AltText)
				markdown += fmt.Sprintf("\n![%s](%s)\n", alt, filename)
			} else {
				markdown += fmt.Sprintf("\n[Watch video](%s)\n", filename)
			}
		}
		title, _, _ := strings.Cut(original.Text, "\n")
		metadata := map[string]any{"title": title, "date": date, "draft": false, "params": map[string]any{"buffer_id": original.ID, "media": bundleMedia}}
		frontmatter, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			return err
		}
		uploads = append(uploads, upload{key: path.Join(updates.Prefix, original.ID, "index.md"), data: append(frontmatter, []byte("\n\n"+markdown)...)})
		posts = append(posts, exported)
	}
	// All downloads succeed before the first write. Publish the index only after every upload succeeds.
	for _, item := range uploads {
		if err := e.store.PutReader(ctx, item.key, bytes.NewReader(item.data), cache.Unconditional()); err != nil {
			return fmt.Errorf("upload %s: %w", item.key, err)
		}
	}
	manifest, err := json.MarshalIndent(posts, "", "  ")
	if err != nil {
		return err
	}
	if err := e.store.Put(ctx, updates.Prefix+"feed.json", string(manifest), cache.Unconditional()); err != nil {
		return fmt.Errorf("publish feed: %w", err)
	}
	return nil
}

func (e exporter) download(ctx context.Context, source string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	// The Buffer bearer token is deliberately absent from media requests.
	response, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download media: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download media: HTTP %d", response.StatusCode)
	}
	return io.ReadAll(response.Body)
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
