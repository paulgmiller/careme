package main

import (
	"careme/internal/config"
	"careme/internal/walmart"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/samber/lo"
)

const defaultConsumerID = "52dae855-d02f-488b-b179-1df6700d7dcf"

func main() {
	var (
		zip          = flag.String("zip", "", "ZIP code to query")
		keyVersion   = flag.String("key-version", envOrDefault("WALMART_KEY_VERSION", "1"), "Walmart key version header")
		baseURL      = flag.String("base-url", walmart.DefaultBaseURL, "Walmart affiliates API base URL")
		privateKey   = flag.String("private-key", envOrDefault("WALMART_PRIVATE_KEY", ""), "path to Walmart private key")
		consumerID   = flag.String("consumer-id", envOrDefault("WALMART_CONSUMER_ID", defaultConsumerID), "Walmart consumer ID")
		categoryName = flag.String("category", "", "Walmart category ID to query taxonomy for")
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

	/*slog.Info("taxonomy request")
	taxonomy, err := client.Taxonomy(ctx)
	if err != nil {
		exitErr(fmt.Errorf("request taxonomy: %w", err))
		return
	}
	for _, category := range taxonomy.Categories {
		fmt.Printf("Category: %s: %s\n", category.Name, category.ID)
		if strings.EqualFold(category.Name, *categoryName) {
			for _, sub := range category.Children {
				fmt.Printf("  Subcategory: %s: %s\n", sub.Name, sub.ID)
			}
		}
	}*/

	var produceBrands = []string{
		//"Freshness Guaranteed", //182
		"Fresh Produce", //10
		"Marketside",    //57
		//"Unbranded",         //27
		"PRODUCE UNBRANDED", //31
	}
	//pulled this out of taxonomy
	var categoryMap = map[string]struct {
		categoryID string
		brands     []string
	}{
		"meat":    {categoryID: "976759_9569500", brands: []string{}},
		"produce": {categoryID: "976759_976793", brands: produceBrands},
	}

	cat, ok := categoryMap[strings.ToLower(*categoryName)]
	if !ok {
		exitErr(fmt.Errorf("unknown category: %s speficy %s", *categoryName, strings.Join(lo.Keys(categoryMap), ", ")))
	}

	stuff, err := client.SearchCatalogByCategory(ctx, cat.categoryID, cat.brands)
	if err != nil {
		exitErr(err)
	}
	fmt.Printf("Found %d items in category\n", len(stuff.Items))
	for _, item := range stuff.Items {
		fmt.Printf("Item: %s: %d, Brand: %s\n", item.Name, item.ItemID, item.BrandName)
	}
	fmt.Printf("total items %d\n", stuff.NumItems)
	brands := lo.GroupBy(stuff.Items, func(i walmart.CatalogProduct) string {
		return i.BrandName
	})
	fmt.Printf("Found %d unique brands in category\n", len(brands))
	for i, brand := range brands {
		if len(brand) < 20 {
			continue
		}
		fmt.Printf("%s :%d\n", i, len(brand))
	}

	if *zip != "" {
		slog.Info("querying Walmart stores", "zip", *zip)
		stores, err := client.SearchStoresByZIP(ctx, *zip)
		if err != nil {
			exitErr(err)
		}

		for _, store := range stores {
			fmt.Printf("Store: %s: %s\n", store.Name, store.StreetAddress)
		}
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
