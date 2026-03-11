package main

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

type zipStoreCount struct {
	Metro string
	Zip   string
	Chain string
	Count int
}

type metroZipCode struct {
	Metro string
	Zip   string
}

func main() {
	var inputPath string
	var outputFormat string
	var timeoutSeconds int

	flag.StringVar(&inputPath, "input", "zipcodes.txt", "Path to CSV/TXT file containing zip codes")
	flag.StringVar(&outputFormat, "format", "csv", "Output format: csv, table, or markdown")
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

	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}

	centroids := locations.LoadCentroids()

	client, err := locations.New(cfg, cacheStore, centroids)
	if err != nil {
		log.Fatalf("failed to create location storage: %v", err)
	}
	wg := sync.WaitGroup{}
	resultsChan := make(chan zipQueryResult, len(metroZipCodes))
	for _, code := range metroZipCodes {
		wg.Add(1)
		go func(mzc metroZipCode) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
			stores, err := client.GetLocationsByZip(ctx, mzc.Zip)
			cancel()
			if err != nil {
				resultsChan <- zipQueryResult{
					err: fmt.Errorf("query locations for zip %s: %w", mzc.Zip, err),
				}
				return
			}
			resultsChan <- zipQueryResult{
				counts: countStoresByChain(mzc, stores),
			}

		}(code)
	}
	wg.Wait()
	close(resultsChan)

	counts := make([]zipStoreCount, 0, len(metroZipCodes))
	for result := range resultsChan {
		if result.err != nil {
			log.Fatal(result.err)
		}
		counts = append(counts, result.counts...)
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Metro != counts[j].Metro {
			return counts[i].Metro < counts[j].Metro
		}
		if counts[i].Zip != counts[j].Zip {
			return counts[i].Zip < counts[j].Zip
		}
		if counts[i].Count != counts[j].Count {
			return counts[i].Count > counts[j].Count
		}
		return counts[i].Chain < counts[j].Chain
	})

	if err := writeCounts(os.Stdout, counts, metroZipCodes, outputFormat); err != nil {
		log.Fatal(err)
	}
}

type zipQueryResult struct {
	counts []zipStoreCount
	err    error
}

func countStoresByChain(mzc metroZipCode, stores []locations.Location) []zipStoreCount {
	counts := make(map[string]int, len(stores))
	for _, store := range stores {
		counts[locationChain(store)]++
	}

	results := make([]zipStoreCount, 0, len(counts))
	for chain, count := range counts {
		results = append(results, zipStoreCount{
			Metro: mzc.Metro,
			Zip:   mzc.Zip,
			Chain: chain,
			Count: count,
		})
	}

	return results
}

func locationChain(store locations.Location) string {
	if chain := normalizeChainName(store.Chain); chain != "" {
		return chain
	}

	if prefix, _, ok := strings.Cut(strings.TrimSpace(store.ID), "_"); ok {
		if chain := normalizeChainName(prefix); chain != "" {
			return chain
		}
	}

	if isAllDigits(strings.TrimSpace(store.ID)) {
		return "kroger"
	}

	return "unknown"
}

func normalizeChainName(raw string) string {
	chain := strings.ToLower(strings.TrimSpace(raw))
	if chain == "" {
		return ""
	}
	return chain
}

func writeCounts(w io.Writer, counts []zipStoreCount, metroZipCodes []metroZipCode, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv":
		return writeCSV(w, counts)
	case "table":
		return writeTable(w, counts, metroZipCodes)
	case "markdown", "md":
		return writeMarkdownTable(w, counts, metroZipCodes)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func writeCSV(w io.Writer, counts []zipStoreCount) error {
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"metro_name", "zip_code", "chain", "store_count"}); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}
	for _, result := range counts {
		if err := writer.Write([]string{
			result.Metro,
			result.Zip,
			result.Chain,
			strconv.Itoa(result.Count),
		}); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	return nil
}

func writeTable(w io.Writer, counts []zipStoreCount, metroZipCodes []metroZipCode) error {
	chains, rows, countsByRow := pivotCounts(counts, metroZipCodes)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := []string{"metro_name", "zip_code"}
	header = append(header, chains...)
	header = append(header, "total")
	if _, err := fmt.Fprintln(tw, strings.Join(header, "\t")); err != nil {
		return fmt.Errorf("write table header: %w", err)
	}

	for _, row := range rows {
		key := rowKey(row)
		line := []string{row.Metro, row.Zip}
		total := 0
		for _, chain := range chains {
			count := countsByRow[key][chain]
			line = append(line, strconv.Itoa(count))
			total += count
		}
		line = append(line, strconv.Itoa(total))
		if _, err := fmt.Fprintln(tw, strings.Join(line, "\t")); err != nil {
			return fmt.Errorf("write table row: %w", err)
		}
	}

	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush table: %w", err)
	}
	return nil
}

func writeMarkdownTable(w io.Writer, counts []zipStoreCount, metroZipCodes []metroZipCode) error {
	chains, rows, countsByRow := pivotCounts(counts, metroZipCodes)

	header := []string{"metro_name", "zip_code"}
	header = append(header, chains...)
	header = append(header, "total")
	if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(header, " | ")); err != nil {
		return fmt.Errorf("write markdown header: %w", err)
	}

	separator := make([]string, len(header))
	for i := range separator {
		separator[i] = "---"
	}
	if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(separator, " | ")); err != nil {
		return fmt.Errorf("write markdown separator: %w", err)
	}

	for _, row := range rows {
		key := rowKey(row)
		line := []string{escapeMarkdownCell(row.Metro), escapeMarkdownCell(row.Zip)}
		total := 0
		for _, chain := range chains {
			count := countsByRow[key][chain]
			line = append(line, strconv.Itoa(count))
			total += count
		}
		line = append(line, strconv.Itoa(total))
		if _, err := fmt.Fprintf(w, "| %s |\n", strings.Join(line, " | ")); err != nil {
			return fmt.Errorf("write markdown row: %w", err)
		}
	}

	return nil
}

type rowKey struct {
	Metro string
	Zip   string
}

func pivotCounts(counts []zipStoreCount, metroZipCodes []metroZipCode) ([]string, []metroZipCode, map[rowKey]map[string]int) {
	chainSet := make(map[string]struct{}, len(counts))
	countsByRow := make(map[rowKey]map[string]int, len(metroZipCodes))
	for _, count := range counts {
		key := rowKey{Metro: count.Metro, Zip: count.Zip}
		if countsByRow[key] == nil {
			countsByRow[key] = make(map[string]int)
		}
		countsByRow[key][count.Chain] = count.Count
		if count.Chain != "" {
			chainSet[count.Chain] = struct{}{}
		}
	}

	chains := make([]string, 0, len(chainSet))
	for chain := range chainSet {
		chains = append(chains, chain)
	}
	sort.Strings(chains)

	rows := append([]metroZipCode(nil), metroZipCodes...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Metro != rows[j].Metro {
			return rows[i].Metro < rows[j].Metro
		}
		return rows[i].Zip < rows[j].Zip
	})

	return chains, rows, countsByRow
}

func escapeMarkdownCell(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
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
