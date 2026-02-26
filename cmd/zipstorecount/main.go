package main

import (
	"careme/internal/config"
	"careme/internal/kroger"
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"
)

type locationClient interface {
	LocationListWithResponse(ctx context.Context, params *kroger.LocationListParams, reqEditors ...kroger.RequestEditorFn) (*kroger.LocationListResponse, error)
}

type zipStoreCount struct {
	Zip   string
	Count int
}

func main() {
	var inputPath string
	var timeoutSeconds int

	flag.StringVar(&inputPath, "input", "zipcodes.txt", "Path to CSV/TXT file containing zip codes")
	flag.IntVar(&timeoutSeconds, "timeout", 20, "HTTP timeout in seconds for each zip query")
	flag.Parse()

	zipCodes, err := readZipCodes(inputPath)
	if err != nil {
		log.Fatalf("failed to read zip codes: %v", err)
	}
	if len(zipCodes) == 0 {
		log.Fatalf("no valid zip codes found in %s", inputPath)
	}

	client, err := newLocationClientFromEnv()
	if err != nil {
		log.Fatalf("failed to initialize Kroger client: %v", err)
	}

	results := make([]zipStoreCount, 0, len(zipCodes))
	for _, zipCode := range zipCodes {
		ctx, cancel := context.WithTimeout(context.Background(), durationFromSeconds(timeoutSeconds))
		count, err := countLocationsByZip(ctx, client, zipCode)
		cancel()
		if err != nil {
			log.Fatalf("failed to query locations for zip %s: %v", zipCode, err)
		}
		results = append(results, zipStoreCount{Zip: zipCode, Count: count})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Count == results[j].Count {
			return results[i].Zip < results[j].Zip
		}
		return results[i].Count < results[j].Count
	})

	fmt.Println("zip_code,store_count")
	for _, result := range results {
		fmt.Printf("%s,%d\n", result.Zip, result.Count)
	}
}

func readZipCodes(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open input file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}

	return extractZipCodes(records)
}

// assume zip code is in second column and ignore header row if present. Normalize zip codes to 5 digits and ignore invalid entries. Remove duplicates.
func extractZipCodes(records [][]string) ([]string, error) {
	if len(records) == 0 {
		return nil, errors.New("empty input file")
	}

	zipCodes := make([]string, 0, len(records))

	for _, row := range records {
		if len(row) < 2 {
			continue
		}

		zipCode := strings.TrimSpace(row[1])
		zipCodes = append(zipCodes, zipCode)
	}

	return lo.Uniq(zipCodes), nil
}

func newLocationClientFromEnv() (locationClient, error) {
	clientID := strings.TrimSpace(os.Getenv("KROGER_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("KROGER_CLIENT_SECRET"))
	if clientID == "" || clientSecret == "" {
		return nil, errors.New("KROGER_CLIENT_ID and KROGER_CLIENT_SECRET must be set")
	}

	cfg := &config.Config{
		Kroger: config.KrogerConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
		},
	}

	client, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func countLocationsByZip(ctx context.Context, client locationClient, zipCode string) (int, error) {
	params := &kroger.LocationListParams{
		FilterZipCodeNear: &zipCode,
	}
	resp, err := client.LocationListWithResponse(ctx, params)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode() != http.StatusOK {
		return 0, fmt.Errorf("status %d: %s", resp.StatusCode(), resp.Status())
	}
	if resp.JSON200 == nil || resp.JSON200.Data == nil {
		return 0, nil
	}
	return len(*resp.JSON200.Data), nil
}

func durationFromSeconds(seconds int) time.Duration {
	if seconds <= 0 {
		return 20 * time.Second
	}
	return time.Duration(seconds) * time.Second
}
