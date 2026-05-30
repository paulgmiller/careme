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
	fs.SetOutput(io.Discard)

	var (
		baseURL     string
		storeID     string
		category    string
		postalCode  string
		zoneID      string
		forterToken string
		first       int
		timeoutSec  int
	)

	fs.StringVar(&baseURL, "base-url", query.DefaultBaseURL, "ALDI base URL")
	fs.StringVar(&storeID, "store-id", "", "ALDI GraphQL shopId to query against")
	fs.StringVar(&category, "category", "", "ALDI collection slug, for example rc-other-fish-18102")
	fs.StringVar(&postalCode, "postal-code", "", "optional postal code")
	fs.StringVar(&zoneID, "zone-id", "", "optional zone id")
	fs.StringVar(&forterToken, "forter-token", envOrDefault("ALDI_FORTER_TOKEN", ""), "optional forterToken cookie value")
	fs.IntVar(&first, "first", 4, "number of products to request")
	fs.IntVar(&timeoutSec, "timeout", 20, "HTTP timeout in seconds")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(storeID) == "" {
		return errors.New("store-id is required")
	}
	if strings.TrimSpace(category) == "" {
		return errors.New("category is required")
	}
	if first < 0 {
		return errors.New("first must be greater than or equal to 0")
	}

	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}

	client := query.NewClient(query.ClientConfig{
		BaseURL:     baseURL,
		ForterToken: forterToken,
		HTTPClient:  newHTTPClient(time.Duration(timeoutSec) * time.Second),
	})

	payload, err := client.CollectionProducts(ctx, storeID, category, query.SearchOptions{
		PostalCode: postalCode,
		ZoneID:     zoneID,
		First:      first,
	})
	if err != nil {
		return fmt.Errorf("query collection products: %w", err)
	}

	items := payload.Data.CollectionProducts.Items
	for i, item := range items {
		if _, err := fmt.Fprintln(out, itemLine(i+1, item)); err != nil {
			return fmt.Errorf("write product: %w", err)
		}
	}
	if _, err := fmt.Fprintf(out, "total products: %d\n", len(items)); err != nil {
		return fmt.Errorf("write total products: %w", err)
	}
	return nil
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
