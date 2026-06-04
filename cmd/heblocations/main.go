package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"careme/internal/albertsons"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/heb"

	"github.com/joho/godotenv"
)

func main() {
	var (
		zip            string
		format         string
		buildID        string
		reese84        string
		timeoutSeconds int
		maxPages       int
		refresh        bool
	)

	flag.StringVar(&zip, "zip", "78204", "ZIP/address search to send to H-E-B store locations")
	flag.StringVar(&format, "format", "table", "output format: table or json")
	flag.StringVar(&buildID, "build-id", "", "HEB Next.js data build ID; defaults to HEB_NEXT_DATA_BUILD_ID, cache, then Bright Data discovery")
	flag.StringVar(&reese84, "reese84", strings.TrimSpace(os.Getenv("HEB_REESE84")), "optional reese84 override; defaults to latest Albertsons cache record")
	flag.IntVar(&timeoutSeconds, "timeout", 20, "HTTP timeout in seconds")
	flag.IntVar(&maxPages, "max-pages", 20, "maximum store-location pages to fetch")
	flag.BoolVar(&refresh, "refresh", false, "ignore cached store-location pages and fetch fresh responses")
	flag.Parse()

	if strings.TrimSpace(zip) == "" {
		log.Fatal("missing required -zip")
	}
	if maxPages <= 0 {
		log.Fatal("-max-pages must be greater than zero")
	}

	if err := loadEnv(); err != nil {
		log.Fatalf("load env: %v", err)
	}

	cacheStore, err := cache.EnsureCache(heb.Container)
	if err != nil {
		log.Fatalf("create heb cache: %v", err)
	}
	albertsonsCache, err := cache.EnsureCache(albertsons.Container)
	if err != nil {
		log.Fatalf("create albertsons cache: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	client := heb.NewStoreLocationsClient(heb.StoreLocationsClientConfig{
		Reese84Provider: reese84Provider(albertsonsCache, reese84),
	})
	loadBuildID := heb.CachedNextDataBuildIDProvider(cacheStore, buildID)
	summaries, err := fetchSummaries(ctx, cacheStore, client, loadBuildID, zip, maxPages, refresh)
	if err != nil {
		log.Fatal(err)
	}

	if err := writeSummaries(os.Stdout, summaries, format); err != nil {
		log.Fatal(err)
	}
}

func reese84Provider(c cache.Cache, override string) func(context.Context) (string, error) {
	override = strings.TrimSpace(override)
	return func(ctx context.Context) (string, error) {
		if override != "" {
			return override, nil
		}
		record, err := albertsons.LoadLatestReese84(ctx, c)
		if err != nil {
			return "", fmt.Errorf("load latest albertsons reese84 cache record: %w", err)
		}
		return record.Cookie, nil
	}
}

func loadEnv() error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	return config.LoadEncryptedEnv("secrets/envtest")
}

type storeLocationsClient interface {
	StoreLocationsPage(ctx context.Context, buildID, address string, page int) ([]byte, error)
}

func fetchSummaries(
	ctx context.Context,
	cacheStore cache.Cache,
	client storeLocationsClient,
	loadBuildID func(context.Context) (string, error),
	address string,
	maxPages int,
	refresh bool,
) ([]heb.StoreSummary, error) {
	seen := make(map[string]struct{})
	summaries := make([]heb.StoreSummary, 0)
	fetchedForAddress := 0

	for pageNumber := 1; pageNumber <= maxPages; pageNumber++ {
		page, err := loadPage(ctx, cacheStore, client, loadBuildID, address, pageNumber, refresh)
		if err != nil {
			return nil, err
		}
		if len(page.Summaries) == 0 {
			break
		}
		for _, summary := range page.Summaries {
			if _, ok := seen[summary.ID]; ok {
				continue
			}
			seen[summary.ID] = struct{}{}
			if err := heb.CacheStoreSummary(ctx, cacheStore, &summary); err != nil {
				return nil, fmt.Errorf("cache store summary %s: %w", summary.ID, err)
			}
			summaries = append(summaries, summary)
		}

		fetchedForAddress += len(page.Summaries)
		if page.TotalStoresCount > 0 && fetchedForAddress >= page.TotalStoresCount {
			break
		}
	}
	return summaries, nil
}

func loadPage(
	ctx context.Context,
	cacheStore cache.Cache,
	client storeLocationsClient,
	loadBuildID func(context.Context) (string, error),
	address string,
	pageNumber int,
	refresh bool,
) (*heb.StoreLocationsPage, error) {
	if !refresh {
		page, err := heb.LoadCachedStoreLocationsPage(ctx, cacheStore, address, pageNumber)
		if err == nil {
			return page, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			log.Printf("ignoring cached H-E-B store-location page %s page %d: %v", address, pageNumber, err)
		}
	}

	buildID, err := loadBuildID(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve H-E-B build ID: %w", err)
	}
	body, err := client.StoreLocationsPage(ctx, buildID, address, pageNumber)
	if err != nil {
		return nil, err
	}
	page, err := heb.DecodeStoreLocationsPage(body)
	if err != nil {
		return nil, err
	}
	if err := heb.CacheStoreLocationsPage(ctx, cacheStore, address, pageNumber, body); err != nil {
		return nil, fmt.Errorf("cache store-location page %s page %d: %w", address, pageNumber, err)
	}
	return page, nil
}

func writeSummaries(out io.Writer, summaries []heb.StoreSummary, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(summaries)
	case "table", "":
		writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(writer, "ID\tSTORE\tNAME\tADDRESS\tCITY\tSTATE\tZIP\tLAT\tLON"); err != nil {
			return err
		}
		for _, summary := range summaries {
			lat, lon := "", ""
			if summary.Lat != nil {
				lat = strconv.FormatFloat(*summary.Lat, 'f', 5, 64)
			}
			if summary.Lon != nil {
				lon = strconv.FormatFloat(*summary.Lon, 'f', 5, 64)
			}
			if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				summary.ID,
				summary.StoreID,
				summary.Name,
				summary.Address,
				summary.City,
				summary.State,
				summary.ZipCode,
				lat,
				lon,
			); err != nil {
				return err
			}
		}
		return writer.Flush()
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}
