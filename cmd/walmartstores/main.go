package main

import (
	"careme/internal/config"
	"careme/internal/walmart"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"
)

const defaultConsumerID = "52dae855-d02f-488b-b179-1df6700d7dcf"

func main() {
	var (
		zip        = flag.String("zip", "98005", "ZIP code to query")
		keyVersion = flag.String("key-version", envOrDefault("WALMART_KEY_VERSION", "1"), "Walmart key version header")
		baseURL    = flag.String("base-url", walmart.DefaultBaseURL, "Walmart affiliates API base URL")
		privateKey = flag.String("private-key", envOrDefault("WALMART_PRIVATE_KEY", ""), "path to Walmart private key")
		consumerID = flag.String("consumer-id", envOrDefault("WALMART_CONSUMER_ID", defaultConsumerID), "Walmart consumer ID")
	)
	flag.Parse()

	client, err := walmart.NewClient(config.WalmartConfig{
		ConsumerID: *consumerID,
		KeyVersion: *keyVersion,
		PrivateKey: *privateKey,
		BaseURL:    *baseURL,
	})
	if err != nil {
		exitErr(fmt.Errorf("create Walmart client: %w", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	//slog.Info("taxonomy request")
	//taxonomy, err := client.Taxonomy(ctx)
	//if err != nil {
	// exitErr(fmt.Errorf("request taxonomy: %w", err))
	//	return
	//}
	//fmt.Printf("taxonomy: %s\n", string(taxonomy))

	slog.Info("querying Walmart stores", "zip", *zip)
	stores, err := client.SearchStoresByZIP(ctx, *zip)
	if err != nil {
		exitErr(err)
	}

	for _, store := range stores {
		fmt.Printf("Store: %s: %s\n", store.Name, store.StreetAddress)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
