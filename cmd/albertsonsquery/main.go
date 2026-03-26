package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"careme/internal/albertsons/query"
)

func main() {
	if err := run(context.Background(), os.Stdout, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, stdout io.Writer, args []string) error {
	return runWithHTTPClient(ctx, stdout, args, nil)
}

func runWithHTTPClient(ctx context.Context, stdout io.Writer, args []string, httpClient *http.Client) error {
	fs := flag.NewFlagSet("albertsonsquery", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		baseURL         string
		storeID         string
		zipCode         string
		subscriptionKey string
		reese84         string
		searchQuery     string
		rows            int
		start           int
		timeoutSec      int
	)

	fs.StringVar(&baseURL, "base-url", query.DefaultSearchBaseURL, "Albertsons-family search base URL")
	fs.StringVar(&storeID, "store-id", "806", "store id to search against")
	fs.StringVar(&zipCode, "zip", "19711", "ZIP code for the store context")
	fs.StringVar(&subscriptionKey, "subscription-key", envOrDefault("ALBERTSONS_SEARCH_SUBSCRIPTION_KEY", "e914eec9448c4d5eb672debf5011cf8f"), "Albertsons pathway subscription key")
	fs.StringVar(&reese84, "reese84", envOrDefault("ALBERTSONS_SEARCH_REESE84", ""), "optional reese84 cookie value")
	fs.StringVar(&searchQuery, "query", "", "search query, default empty string")
	fs.IntVar(&rows, "rows", 60, "number of rows to request")
	fs.IntVar(&start, "start", 0, "pagination start offset")
	fs.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if storeID == "" {
		return errors.New("store-id is required")
	}
	if zipCode == "" {
		return errors.New("zip is required")
	}
	if subscriptionKey == "" {
		return errors.New("subscription-key is required")
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
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

	resp, err := client.Search(ctx, storeID, zipCode, query.SearchOptions{
		Query: searchQuery,
		Start: start,
		Rows:  rows,
	})
	if err != nil {
		return fmt.Errorf("run search: %w", err)
	}

	var payload query.PathwaySearchPayload
	if err := resp.DecodeJSON(&payload); err != nil {
		return fmt.Errorf("decode search response: %w", err)
	}

	_, err = fmt.Fprintln(stdout, len(payload.Response.Docs))
	for i, doc := range payload.Response.Docs {
		_, _ = fmt.Fprintf(stdout, "%d: %s (price: %.2f)\n", i+1, doc.Name, doc.Price)
	}
	return err
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
