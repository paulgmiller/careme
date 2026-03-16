package main

import (
	"careme/internal/browserrendering"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultPrompt = "Extract the actual products or listings visible on each crawled page. " +
	"Return only items someone could buy, not navigation, promo copy, coupons, banners, recipes, or marketing text. " +
	"Map each item into a Kroger-style ingredient shape. Use id only if a product id is clearly present. " +
	"Use number only if an aisle number is clearly present. Use regularPrice and salePrice as numbers when readable. " +
	"Use categories for the product family when the page makes it clear."

type extractedProduct struct {
	SourceURL    string   `json:"source_url,omitempty"`
	ID           string   `json:"id,omitempty"`
	Number       string   `json:"number,omitempty"`
	Brand        string   `json:"brand,omitempty"`
	Description  string   `json:"description"`
	SalePrice    *float64 `json:"salePrice,omitempty"`
	RegularPrice *float64 `json:"regularPrice,omitempty"`
	Size         string   `json:"size,omitempty"`
	Categories   []string `json:"categories,omitempty"`
	ProductURL   string   `json:"productURL,omitempty"`
	ImageURL     string   `json:"imageURL,omitempty"`
	Availability string   `json:"availability,omitempty"`
}

type crawlProducts struct {
	Products []extractedProduct `json:"products"`
}

type pageOutput struct {
	URL          string          `json:"url"`
	Status       string          `json:"status"`
	HTTPStatus   int             `json:"http_status,omitempty"`
	Title        string          `json:"title,omitempty"`
	ProductCount int             `json:"product_count,omitempty"`
	JSON         json.RawMessage `json:"json,omitempty"`
}

type crawlOutput struct {
	JobID              string             `json:"job_id"`
	Status             string             `json:"status"`
	BrowserSecondsUsed float64            `json:"browser_seconds_used,omitempty"`
	Total              int                `json:"total,omitempty"`
	Finished           int                `json:"finished,omitempty"`
	Pages              []pageOutput       `json:"pages"`
	Products           []extractedProduct `json:"products"`
}

func main() {
	var (
		rawURL        string
		accountID     string
		apiToken      string
		depth         int
		limit         int
		waitUntil     string
		timeoutMS     int
		pollDelaySec  int
		pollAttempts  int
		source        string
		includeRaw    string
		excludeRaw    string
		includeSubdom bool
		includeExt    bool
		render        bool
		prompt        string
	)

	flag.StringVar(&rawURL, "url", "", "URL to crawl")
	flag.StringVar(&accountID, "account-id", firstNonEmpty(os.Getenv("CLOUDFLARE_ACCOUNT_ID"), os.Getenv("CF_ACCOUNT_ID")), "Cloudflare account ID")
	flag.StringVar(&apiToken, "api-token", firstNonEmpty(os.Getenv("CLOUDFLARE_API_TOKEN"), os.Getenv("CF_API_TOKEN")), "Cloudflare API token")
	flag.IntVar(&depth, "depth", 3, "Maximum crawl depth")
	flag.IntVar(&limit, "limit", 25, "Maximum pages to crawl")
	flag.StringVar(&waitUntil, "wait-until", "networkidle0", "Browser waitUntil mode")
	flag.IntVar(&timeoutMS, "timeout-ms", 60000, "Page timeout in milliseconds")
	flag.IntVar(&pollDelaySec, "poll-delay-sec", 5, "Seconds between crawl status polls")
	flag.IntVar(&pollAttempts, "poll-attempts", 60, "Maximum number of crawl status polls")
	flag.StringVar(&source, "source", "links", "Crawl source: all, sitemaps, or links")
	flag.StringVar(&includeRaw, "include-patterns", "", "Comma-separated include patterns")
	flag.StringVar(&excludeRaw, "exclude-patterns", "", "Comma-separated exclude patterns")
	flag.BoolVar(&includeSubdom, "include-subdomains", false, "Follow links to subdomains")
	flag.BoolVar(&includeExt, "include-external-links", false, "Follow links to external domains")
	flag.BoolVar(&render, "render", true, "Use browser rendering")
	flag.StringVar(&prompt, "prompt", defaultPrompt, "Extraction prompt for the JSON crawl format")
	flag.Parse()

	if strings.TrimSpace(rawURL) == "" {
		fatalf("missing required -url")
	}
	if strings.TrimSpace(accountID) == "" {
		fatalf("missing Cloudflare account ID; set -account-id or CLOUDFLARE_ACCOUNT_ID")
	}
	if strings.TrimSpace(apiToken) == "" {
		fatalf("missing Cloudflare API token; set -api-token or CLOUDFLARE_API_TOKEN")
	}

	client, err := browserrendering.NewClient(accountID, apiToken, &http.Client{Timeout: 90 * time.Second})
	if err != nil {
		fatalf("create browser rendering client: %v", err)
	}

	ctx := context.Background()
	jobID, err := client.StartCrawl(ctx, buildCrawlRequest(
		rawURL,
		depth,
		limit,
		source,
		render,
		waitUntil,
		timeoutMS,
		prompt,
		splitCSV(includeRaw),
		splitCSV(excludeRaw),
		includeSubdom,
		includeExt,
	))
	if err != nil {
		fatalf("start crawl: %v", err)
	}

	_, err = client.WaitForCrawl(ctx, jobID, browserrendering.CrawlWaitOptions{
		MaxAttempts: pollAttempts,
		Delay:       time.Duration(pollDelaySec) * time.Second,
		PollLimit:   1,
	})
	if err != nil {
		fatalf("wait for crawl: %v", err)
	}

	job, err := client.GetCrawlAll(ctx, jobID)
	if err != nil {
		fatalf("fetch crawl results: %v", err)
	}

	output := summarizeCrawl(job)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fatalf("encode output: %v", err)
	}
}

func buildCrawlRequest(
	rawURL string,
	depth int,
	limit int,
	source string,
	render bool,
	waitUntil string,
	timeoutMS int,
	prompt string,
	includePatterns []string,
	excludePatterns []string,
	includeSubdomains bool,
	includeExternalLinks bool,
) browserrendering.CrawlRequest {
	req := browserrendering.CrawlRequest{
		URL:     strings.TrimSpace(rawURL),
		Depth:   depth,
		Limit:   limit,
		Source:  strings.TrimSpace(source),
		Formats: []string{"json"},
		Render:  &render,
		GotoOptions: &browserrendering.GotoOptions{
			WaitUntil: strings.TrimSpace(waitUntil),
			Timeout:   timeoutMS,
		},
		JSONOptions: &browserrendering.JSONOptions{
			Prompt:         strings.TrimSpace(prompt),
			ResponseFormat: defaultProductResponseFormat(),
		},
		Options: &browserrendering.CrawlOptions{
			IncludeExternalLinks: &includeExternalLinks,
			IncludeSubdomains:    &includeSubdomains,
			IncludePatterns:      includePatterns,
			ExcludePatterns:      excludePatterns,
		},
	}
	return req
}

func defaultProductResponseFormat() *browserrendering.ResponseFormat {
	return &browserrendering.ResponseFormat{
		Type: "json_schema",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"products": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":           map[string]any{"type": "string"},
							"number":       map[string]any{"type": "string"},
							"brand":        map[string]any{"type": "string"},
							"description":  map[string]any{"type": "string"},
							"salePrice":    map[string]any{"type": "number"},
							"regularPrice": map[string]any{"type": "number"},
							"size":         map[string]any{"type": "string"},
							"categories": map[string]any{
								"type":  "array",
								"items": map[string]any{"type": "string"},
							},
							"productURL":   map[string]any{"type": "string"},
							"imageURL":     map[string]any{"type": "string"},
							"availability": map[string]any{"type": "string"},
						},
						"required": []string{"description"},
					},
				},
			},
			"required": []string{"products"},
		},
	}
}

func summarizeCrawl(job *browserrendering.CrawlJob) crawlOutput {
	if job == nil {
		return crawlOutput{}
	}

	output := crawlOutput{
		JobID:              job.ID,
		Status:             job.Status,
		BrowserSecondsUsed: job.BrowserSecondsUsed,
		Total:              job.Total,
		Finished:           job.Finished,
		Pages:              make([]pageOutput, 0, len(job.Records)),
		Products:           make([]extractedProduct, 0),
	}

	for _, record := range job.Records {
		page := pageOutput{
			URL:        record.URL,
			Status:     record.Status,
			HTTPStatus: record.Metadata.Status,
			Title:      record.Metadata.Title,
		}
		if len(record.JSON) > 0 {
			page.JSON = append(json.RawMessage(nil), record.JSON...)
			var parsed crawlProducts
			if err := json.Unmarshal(record.JSON, &parsed); err == nil {
				page.ProductCount = len(parsed.Products)
				for _, product := range parsed.Products {
					product.SourceURL = record.URL
					output.Products = append(output.Products, product)
				}
			}
		}
		output.Pages = append(output.Pages, page)
	}

	return output
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
