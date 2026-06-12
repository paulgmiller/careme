package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/recipes"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const defaultLimit = 15

type scoreRow struct {
	Rank            int
	Location        locations.Location
	SupportsStaples bool
	IngredientCount int
	ProduceScore    *locations.ProduceScore
	Error           error
}

func main() {
	var zip string
	var limit int

	flag.StringVar(&zip, "zip", "", "ZIP code to search")
	flag.IntVar(&limit, "n", defaultLimit, "Number of top locations to fetch and score")
	flag.Parse()

	if zip == "" && flag.NArg() > 0 {
		zip = flag.Arg(0)
	}
	zip = strings.TrimSpace(zip)
	if zip == "" {
		log.Fatal("provide a ZIP code with -zip 98101 or as the first argument")
	}
	if limit <= 0 {
		log.Fatal("-n must be greater than 0")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}
	closeLogs, err := logsetup.Configure(ctx)
	if err != nil {
		log.Fatalf("failed to configure logging: %v", err)
	}
	defer closeLogs()

	cacheStore, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}
	centroids := locations.LoadCentroids()
	locationStorage, err := locations.New(cfg, cacheStore, centroids)
	if err != nil {
		log.Fatalf("failed to create location storage: %v", err)
	}
	grader := ingredientgrading.NewManager(cfg, cacheStore, &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)})
	staples, err := recipes.NewCachedStaplesService(cfg, cacheStore, grader)
	if err != nil {
		log.Fatalf("failed to create staples service: %v", err)
	}

	locs, err := locationStorage.GetLocationsByZip(ctx, zip)
	if err != nil {
		log.Fatalf("failed to get locations for zip %s: %v", zip, err)
	}

	rows := scoreLocations(ctx, locs, limit, locationStorage.HasInventory, staples, recipes.NewCachedProduceScorer(recipes.IO(cacheStore)))
	printRows(os.Stdout, rows)

	if err := rowsErr(rows); err != nil {
		log.Fatalf("one or more locations failed: %v", err)
	}
}

type inventoryLookup func(string) bool

type staplesFetcher interface {
	FetchStaples(ctx context.Context, p *recipes.GeneratorParams) ([]ai.InputIngredient, error)
}

func scoreLocations(
	ctx context.Context,
	locs []locations.Location,
	limit int,
	hasInventory inventoryLookup,
	staples staplesFetcher,
	scorer *recipes.CachedProduceScorer,
) []scoreRow {
	selected := topLocations(locs, limit)
	rows := make([]scoreRow, 0, len(selected))
	for i, loc := range selected {
		row := scoreRow{
			Rank:            i + 1,
			Location:        loc,
			SupportsStaples: hasInventory(loc.ID),
		}
		if !row.SupportsStaples {
			rows = append(rows, row)
			continue
		}

		date, err := recipes.StoreToDate(ctx, time.Now(), &loc)
		if err != nil {
			row.Error = err
			rows = append(rows, row)
			continue
		}

		ingredients, err := staples.FetchStaples(ctx, recipes.DefaultParams(&loc, date))
		if err != nil {
			row.Error = err
			rows = append(rows, row)
			continue
		}

		row.IngredientCount = len(ingredients)
		row.ProduceScore = scorer.ProduceScore(ctx, loc)
		rows = append(rows, row)
	}
	return rows
}

func topLocations(locs []locations.Location, limit int) []locations.Location {
	if limit <= 0 || len(locs) == 0 {
		return nil
	}
	if limit > len(locs) {
		limit = len(locs)
	}
	return locs[:limit]
}

func printRows(out *os.File, rows []scoreRow) {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "RANK\tID\tCHAIN\tNAME\tZIP\tINGREDIENTS\tPRODUCE_SCORE\tDATE\tSTATUS")
	for _, row := range rows {
		score := ""
		scoreDate := ""
		status := "ok"
		switch {
		case !row.SupportsStaples:
			status = "unsupported"
		case row.Error != nil:
			status = row.Error.Error()
		case row.ProduceScore == nil:
			status = "score unavailable"
		default:
			score = fmt.Sprintf("%d", row.ProduceScore.Score)
			scoreDate = row.ProduceScore.Date.Format("2006-01-02")
		}

		_, _ = fmt.Fprintf(
			writer,
			"%d\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			row.Rank,
			row.Location.ID,
			row.Location.Chain,
			row.Location.Name,
			row.Location.ZipCode,
			row.IngredientCount,
			score,
			scoreDate,
			status,
		)
	}
	_ = writer.Flush()
}

func rowsErr(rows []scoreRow) error {
	var errs []error
	for _, row := range rows {
		if row.Error != nil {
			errs = append(errs, fmt.Errorf("%s: %w", row.Location.ID, row.Error))
		}
	}
	return errors.Join(errs...)
}
