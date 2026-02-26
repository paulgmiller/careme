package main

import (
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/locations"
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

type locationClient interface {
	LocationListWithResponse(ctx context.Context, params *kroger.LocationListParams, reqEditors ...kroger.RequestEditorFn) (*kroger.LocationListResponse, error)
}

type zipStoreCount struct {
	Metro string
	Zip   string
	Count int
}

type metroZipCode struct {
	Metro string
	Zip   string
}

func main() {
	var inputPath string
	var timeoutSeconds int

	flag.StringVar(&inputPath, "input", "zipcodes.txt", "Path to CSV/TXT file containing zip codes")
	flag.IntVar(&timeoutSeconds, "timeout", 20, "HTTP timeout in seconds for each zip query")
	flag.Parse()

	metroZipCodes, err := readZipCodes(inputPath)
	if err != nil {
		log.Fatalf("failed to read zip codes: %v", err)
	}
	if len(metroZipCodes) == 0 {
		log.Fatalf("no valid zip codes found in %s", inputPath)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	client, err := locations.New(cfg) // warm up location client
	if err != nil {
		log.Fatalf("failed to create location storage: %v", err)
	}
	results := make([]zipStoreCount, 0, len(metroZipCodes))
	for _, metroZipCode := range metroZipCodes {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		stores, err := client.GetLocationsByZip(ctx, metroZipCode.Zip)
		cancel()
		if err != nil {
			log.Fatalf("failed to query locations for zip %s: %v", metroZipCode.Zip, err)
		}
		results = append(results, zipStoreCount{Metro: metroZipCode.Metro, Zip: metroZipCode.Zip, Count: len(stores)})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Count == results[j].Count {
			return results[i].Zip < results[j].Zip
		}
		return results[i].Count < results[j].Count
	})

	fmt.Println("metro_name,zip_code,store_count")
	for _, result := range results {
		fmt.Printf("%s,%s,%d\n", result.Metro, result.Zip, result.Count)
	}
}

func readZipCodes(path string) ([]metroZipCode, error) {
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

// assume metro is in the first column and zip code is in the second column.
// Ignore header row if present. Normalize zip codes to 5 digits and ignore invalid entries.
// Remove duplicate zip codes and keep the first metro seen for each zip.
func extractZipCodes(records [][]string) ([]metroZipCode, error) {
	if len(records) == 0 {
		return nil, errors.New("empty input file")
	}

	zipCodes := make([]metroZipCode, 0, len(records))
	seen := make(map[string]struct{}, len(records))

	for _, row := range records {
		if len(row) < 2 {
			continue
		}

		metroName := strings.TrimSpace(row[0])
		zipCode, ok := normalizeZipCode(row[1])
		if !ok {
			continue
		}

		if _, exists := seen[zipCode]; exists {
			continue
		}
		seen[zipCode] = struct{}{}
		zipCodes = append(zipCodes, metroZipCode{Metro: metroName, Zip: zipCode})
	}

	return zipCodes, nil
}

func normalizeZipCode(raw string) (string, bool) {
	zipCode := strings.TrimSpace(raw)

	if len(zipCode) == 5 && isAllDigits(zipCode) {
		return zipCode, true
	}
	if len(zipCode) == 10 && zipCode[5] == '-' && isAllDigits(zipCode[:5]) && isAllDigits(zipCode[6:]) {
		return zipCode[:5], true
	}

	return "", false
}

func isAllDigits(value string) bool {
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
