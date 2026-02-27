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

type fixedProductID struct {
	Label string
	ID    string
}

var defaultFixedProductIDs = []fixedProductID{
	{Label: "broccoli", ID: "51259378"},
	{Label: "carrots", ID: "10535757"},
	{Label: "thin ribeye", ID: "51259038"},
	{Label: "london broil", ID: "149141521"},
	{Label: "chicken thighs", ID: "778091348"},
}

func main() {
	var (
		zip             = flag.String("zip", "", "ZIP code to query")
		storeID         = flag.String("store-id", "3039", "Store ID to query product lookup against (ignored when --zip is set)")
		keyVersion      = flag.String("key-version", envOrDefault("WALMART_KEY_VERSION", "1"), "Walmart key version header")
		baseURL         = flag.String("base-url", walmart.DefaultBaseURL, "Walmart affiliates API base URL")
		privateKey      = flag.String("private-key", envOrDefault("WALMART_PRIVATE_KEY", ""), "path to Walmart private key")
		consumerID      = flag.String("consumer-id", envOrDefault("WALMART_CONSUMER_ID", defaultConsumerID), "Walmart consumer ID")
		categoryName    = flag.String("category", "", "Walmart category ID to query taxonomy for")
		fixedProductIDs = flag.Bool("fixed-product-ids", false, "Lookup a built-in set of product IDs instead of querying catalog categories")
		timeout         = flag.Duration("timeout", 5*time.Minute, "overall timeout for Walmart calls")
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

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
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

	var meatBrands = []string{
		"NOBRAND",           // 12
		"Unbranded",         // 27
		"Walmart Seafood",   // 13
		"Fresh Pork",        // 20
		"Fresh Beef",        // 17
		"Foster Farms",      // 17
		"WHOLE MUSCLE BEEF", // 16
		"",                  // 16
	}
	//pulled this out of taxonomy
	var categoryMap = map[string]struct {
		categoryID string
		brands     []string
	}{
		"meat":    {categoryID: "976759_9569500", brands: meatBrands},
		"produce": {categoryID: "976759_976793", brands: produceBrands},
	}

	var failures int
	var unresolved int
	var resolved int
	var inStock int

	if *fixedProductIDs {
		fmt.Printf("Using %d fixed product IDs\n", len(defaultFixedProductIDs))
		for _, item := range defaultFixedProductIDs {
			var (
				results *walmart.ProductLookupResults
				err     error
			)
			if strings.TrimSpace(*zip) != "" {
				results, err = client.ProductLookupByZIP(ctx, []string{item.ID}, *zip)
			} else {
				results, err = client.ProductLookup(ctx, []string{item.ID}, *storeID)
			}
			if err != nil {
				slog.Error("product lookup failed", "itemID", item.ID, "label", item.Label, "error", err)
				failures++
				continue
			}
			if len(results.Items) == 0 {
				unresolved++
				continue
			}
			resolved += len(results.Items)
			for _, result := range results.Items {
				if strings.EqualFold(strings.TrimSpace(result.Stock), "available") {
					inStock++
				}
				fmt.Printf("Item: %s: %d, Stock: %s (requested: %s)\n", result.Name, result.ItemID, result.Stock, item.Label)
			}
		}
	} else {
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
			var (
				results *walmart.ProductLookupResults
				err     error
			)
			if strings.TrimSpace(*zip) != "" {
				results, err = client.ProductLookupCatalogItemByZIP(ctx, item, *zip)
			} else {
				results, err = client.ProductLookupCatalogItem(ctx, item, *storeID)
			}
			if err != nil {
				slog.Error("product lookup failed", "itemID", item.ItemID, "name", item.Name, "error", err)
				failures++
				continue
			}
			if len(results.Items) == 0 {
				unresolved++
				continue
			}
			resolved += len(results.Items)
			for _, item := range results.Items {
				if strings.EqualFold(strings.TrimSpace(item.Stock), "available") {
					inStock++
				}
				fmt.Printf("Item: %s: %d, Stock: %s\n", item.Name, item.ItemID, item.Stock)
			}
		}
	}
	fmt.Printf("Failed lookups: %d\n", failures)
	fmt.Printf("Resolved items: %d\n", resolved)
	fmt.Printf("In-stock items: %d\n", inStock)
	fmt.Printf("Unresolved lookups: %d\n", unresolved)
	/*fmt.Printf("total items %d\n", stuff.NumItems)
	brands := lo.GroupBy(stuff.Items, func(i walmart.CatalogProduct) string {
		return i.BrandName
	})
	fmt.Printf("Found %d unique brands in category\n", len(brands))
	for i, brand := range brands {
		if len(brand) < 10 {
			continue
		}
		fmt.Printf("%s :%d\n", i, len(brand))
	}
	*/

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
