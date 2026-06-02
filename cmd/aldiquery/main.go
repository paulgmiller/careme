package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"careme/internal/aldi/query"
)

const itemBatchSize = 10

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

var newHTTPClient = func(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("aldiquery", flag.ContinueOnError)

	var (
		baseURL            string
		storeID            string
		slug               string
		postalCode         string
		zoneID             string
		instacartSID       string
		instacartSessionID string
		cookieHeader       string
		first              int
		timeoutSec         int
		debug              bool
	)

	fs.StringVar(&baseURL, "base-url", query.DefaultBaseURL, "ALDI base URL")
	fs.StringVar(&storeID, "store-id", "", "ALDI GraphQL shopId to query against")
	fs.StringVar(&slug, "slug", "", "ALDI collection slug, for example n-beef-67693")
	fs.StringVar(&slug, "category", "", "ALDI collection slug, for example n-beef-67693")
	fs.StringVar(&postalCode, "postal-code", "", "optional postal code")
	fs.StringVar(&zoneID, "zone-id", "", "optional zone id")
	fs.StringVar(&instacartSID, "instacart-sid", envOrDefault("ALDI_INSTACART_SID", ""), "optional __Host-instacart_sid cookie override")
	fs.StringVar(&instacartSessionID, "instacart-session-id", envOrDefault("ALDI_INSTACART_SESSION_ID", ""), "optional _instacart_session_id cookie override")
	fs.StringVar(&cookieHeader, "cookie", envOrDefault("ALDI_COOKIE", ""), "optional raw Cookie header override copied from a browser request")
	fs.IntVar(&first, "first", 4, "number of products to request")
	fs.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")
	fs.BoolVar(&debug, "debug", false, "print ALDI request diagnostics to stderr")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(storeID) == "" {
		return errors.New("store-id is required")
	}
	if strings.TrimSpace(slug) == "" {
		return errors.New("slug is required")
	}
	if first < 0 {
		return errors.New("first must be greater than or equal to 0")
	}

	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}

	clientConfig := query.ClientConfig{
		BaseURL:            baseURL,
		InstacartSID:       instacartSID,
		InstacartSessionID: instacartSessionID,
		CookieHeader:       cookieHeader,
		HTTPClient:         newHTTPClient(time.Duration(timeoutSec) * time.Second),
	}
	if debug {
		clientConfig.DebugWriter = os.Stderr
	}
	client := query.NewClient(clientConfig)

	payload, err := client.CollectionProducts(ctx, storeID, slug, query.SearchOptions{
		PostalCode: postalCode,
		ZoneID:     zoneID,
		First:      first,
	})
	if err != nil {
		return fmt.Errorf("query collection products: %w", err)
	}

	items := payload.Data.Items()
	itemIDs := payload.Data.ItemIDs()
	if len(itemIDs) > len(items) {
		items, err = hydrateItems(ctx, client, storeID, itemIDs, query.SearchOptions{
			PostalCode: postalCode,
			ZoneID:     zoneID,
		}, first)
		if err != nil {
			return fmt.Errorf("hydrate collection products: %w", err)
		}
	}
	for i, item := range items {
		if _, err := fmt.Fprintln(out, itemLine(i+1, item)); err != nil {
			return fmt.Errorf("write product: %w", err)
		}
	}
	if len(items) == 0 {
		for i, itemID := range itemIDs {
			if _, err := fmt.Fprintf(out, "%d: item_id=%s\n", i+1, itemID); err != nil {
				return fmt.Errorf("write item id: %w", err)
			}
		}
	}

	total := len(items)
	if total == 0 {
		total = len(itemIDs)
	}
	if _, err := fmt.Fprintf(out, "total products: %d\n", total); err != nil {
		return fmt.Errorf("write total products: %w", err)
	}
	return nil
}

func hydrateItems(ctx context.Context, client *query.Client, storeID string, itemIDs []string, opts query.SearchOptions, limit int) ([]query.Item, error) {
	if limit > 0 && len(itemIDs) > limit {
		itemIDs = itemIDs[:limit]
	}

	items := make([]query.Item, 0, len(itemIDs))
	for start := 0; start < len(itemIDs); start += itemBatchSize {
		end := start + itemBatchSize
		if end > len(itemIDs) {
			end = len(itemIDs)
		}

		payload, err := client.Items(ctx, storeID, itemIDs[start:end], opts)
		if err != nil {
			return nil, err
		}
		items = append(items, payload.Data.Items...)
	}
	return items, nil
}

func itemLine(index int, item query.Item) string {
	var line strings.Builder
	line.WriteString(strconv.Itoa(index))
	line.WriteString(": ")
	line.WriteString(item.Name)
	if item.Size != "" {
		line.WriteString(" (")
		line.WriteString(item.Size)
		line.WriteString(")")
	}
	if price := displayPrice(item); price != "" {
		line.WriteString(" - ")
		line.WriteString(price)
	}
	if unitPrice := item.Price.ViewSection.ItemCard.PricingUnitString; unitPrice != "" {
		line.WriteString(" [")
		line.WriteString(unitPrice)
		line.WriteString("]")
	}
	if item.Availability.StockLevel != "" {
		line.WriteString(" ")
		line.WriteString(item.Availability.StockLevel)
	}
	if item.ProductID != "" {
		line.WriteString(" product=")
		line.WriteString(item.ProductID)
	}
	return line.String()
}

func displayPrice(item query.Item) string {
	if item.Price.ViewSection.ItemCard.PriceString != "" {
		return item.Price.ViewSection.ItemCard.PriceString
	}
	return item.Price.ViewSection.PriceString
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
