package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"careme/internal/aldi/query"
)

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
		baseURL    string
		storeID    string
		slug       string
		postalCode string
		zoneID     string
		first      int
		timeoutSec int
	)

	fs.StringVar(&baseURL, "base-url", query.DefaultBaseURL, "ALDI base URL")
	fs.StringVar(&storeID, "store-id", "", "ALDI GraphQL shopId to query against")
	fs.StringVar(&slug, "slug", "", "ALDI collection slug, for example n-beef-67693")
	fs.StringVar(&slug, "category", "", "ALDI collection slug, for example n-beef-67693")
	fs.StringVar(&postalCode, "postal-code", "", "postal code")
	fs.StringVar(&zoneID, "zone-id", "", "optional zone id")
	fs.IntVar(&first, "first", 4, "number of products to request")
	fs.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(storeID) == "" {
		return errors.New("store-id is required")
	}
	if strings.TrimSpace(slug) == "" {
		return errors.New("slug is required")
	}
	if strings.TrimSpace(postalCode) == "" {
		return errors.New("postal-code is required")
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
		BaseURL:    baseURL,
		HTTPClient: newHTTPClient(time.Duration(timeoutSec) * time.Second),
	}
	client := query.NewClient(clientConfig)

	items, err := client.Products(ctx, storeID, postalCode, slug, query.SearchOptions{
		ZoneID: zoneID,
		First:  first,
	})
	if err != nil {
		return fmt.Errorf("query collection products: %w", err)
	}

	for _, item := range items {
		if _, err := fmt.Fprintln(out, itemLine(item)); err != nil {
			return fmt.Errorf("write product: %w", err)
		}
	}
	if _, err := fmt.Fprintf(out, "total products: %d\n", len(items)); err != nil {
		return fmt.Errorf("write total products: %w", err)
	}
	return nil
}

func itemLine(item query.Item) string {
	var line strings.Builder
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
