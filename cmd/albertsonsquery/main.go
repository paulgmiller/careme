package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"careme/internal/albertsons/query"
	"careme/internal/brightdata"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("albertsonsquery", flag.ExitOnError)

	var (
		baseURL         string
		storeID         string
		subscriptionKey string
		reese84         string
		searchQuery     string
		rows            uint
		start           uint
		timeoutSec      int
	)

	// get from store_id?
	fs.StringVar(&baseURL, "base-url", query.DefaultSearchBaseURL, "Albertsons-family search base URL")
	fs.StringVar(&storeID, "store-id", "806", "store id to search against")
	fs.StringVar(&subscriptionKey, "subscription-key", envOrDefault("ALBERTSONS_SEARCH_SUBSCRIPTION_KEY", ""), "Albertsons pathway subscription key")
	fs.StringVar(&reese84, "reese84", envOrDefault("ALBERTSONS_SEARCH_REESE84", ""), "optional reese84 cookie value")
	fs.StringVar(&searchQuery, "query", "", "search query, default empty string")
	fs.UintVar(&rows, "rows", 60, "number of rows to request")
	fs.UintVar(&start, "start", 0, "pagination start offset")
	fs.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if storeID == "" {
		return errors.New("store-id is required")
	}
	if subscriptionKey == "" {
		return errors.New("subscription-key is required")
	}
	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}
	httpClient, err := brightdata.NewProxyAwareHTTPClient(brightdata.LoadConfig())
	if err != nil {
		return fmt.Errorf("create HTTP client: %w", err)
	}
	client, err := query.NewSearchClient(query.SearchClientConfig{
		BaseURL:         baseURL,
		SubscriptionKey: subscriptionKey,
		Reese84:         reese84,
		HTTPClient:      httpClient,
	})
	if err != nil {
		return fmt.Errorf("create search client: %w", err)
	}

	payload, err := client.Search(ctx, storeID, query.Category_Vegatables, query.SearchOptions{
		Query: searchQuery,
		Start: start,
		Rows:  rows,
	})
	if err != nil {
		return fmt.Errorf("run search: %w", err)
	}

	for i, doc := range payload.Response.Docs {
		_, _ = fmt.Printf("%d: %s (price: %.2f)\n", i+1, doc.Name, doc.Price)
	}
	_, err = fmt.Printf("total products: %d\n", len(payload.Response.Docs))
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
