package main

import (
	"context"
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
	"careme/internal/parallelism"
	"careme/internal/recipes"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const defaultLimit = 15

type scoreRow struct {
	Location        locations.Location
	SupportsStaples bool
	IngredientCount int
	ProduceScore    *locations.ProduceScore
	Error           error
}

func main() {
	var zip string
	var limit int
	var useStaplesWatchdogLocations bool

	flag.StringVar(&zip, "zip", "", "ZIP code to search")
	flag.IntVar(&limit, "n", defaultLimit, "Number of top locations to fetch and score")
	flag.BoolVar(&useStaplesWatchdogLocations, "staples-watchdog-locations", false, "Use the store IDs checked by the staples watchdog instead of a ZIP search")
	flag.Parse()

	if zip == "" && flag.NArg() > 0 {
		zip = flag.Arg(0)
	}
	zip = strings.TrimSpace(zip)
	if zip == "" && !useStaplesWatchdogLocations {
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

	locs, err := locationsToScore(ctx, locationStorage, zip, useStaplesWatchdogLocations)
	if err != nil {
		if useStaplesWatchdogLocations {
			log.Fatalf("failed to get staples watchdog locations: %v", err)
		}
		log.Fatalf("failed to get locations for zip %s: %v", zip, err)
	}

	rows, err := scoreLocations(ctx, locs, limit, locationStorage.HasInventory, staples, recipes.NewCachedProduceScorer(recipes.IO(cacheStore)))
	printRows(os.Stdout, rows)
	if err != nil {
		log.Fatalf("one or more locations failed: %v", err)
	}
}

type inventoryLookup func(string) bool

type zipLocationLookup interface {
	GetLocationsByZip(ctx context.Context, zipcode string) ([]locations.Location, error)
}

type staplesFetcher interface {
	FetchStaples(ctx context.Context, p *recipes.GeneratorParams) ([]ai.InputIngredient, error)
}

func locationsToScore(ctx context.Context, lookup zipLocationLookup, zip string, useStaplesWatchdogLocations bool) ([]locations.Location, error) {
	if useStaplesWatchdogLocations {
		return recipes.StaplesWatchdogLocations(), nil
	}
	return lookup.GetLocationsByZip(ctx, zip)
}

func scoreLocations(
	ctx context.Context,
	locs []locations.Location,
	limit int,
	hasInventory inventoryLookup,
	staples staplesFetcher,
	scorer *recipes.CachedProduceScorer,
) ([]scoreRow, error) {
	selected := topLocations(locs, limit)
	return parallelism.MapWithErrors(selected, func(loc locations.Location) (scoreRow, error) {
		row := scoreRow{
			Location:        loc,
			SupportsStaples: hasInventory(loc.ID),
		}
		if !row.SupportsStaples {
			return row, nil
		}

		date, err := recipes.StoreToDate(ctx, time.Now(), &loc)
		if err != nil {
			return row, err
		}

		ingredients, err := staples.FetchStaples(ctx, recipes.DefaultParams(&loc, date))
		if err != nil {
			return row, err
		}

		row.IngredientCount = len(ingredients)
		row.ProduceScore = scorer.ProduceScore(ctx, loc)
		return row, nil
	})
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
	_, _ = fmt.Fprintln(writer, "ID\tCHAIN\tNAME\tZIP\tINGREDIENTS\tPRODUCE_SCORE\tDATE\tSTATUS")
	for _, row := range rows {
		score := ""
		scoreDate := ""
		status := "ok"
		switch {
		case !row.SupportsStaples:
			status = "unsupported"
		case row.ProduceScore == nil:
			status = "score unavailable"
		default:
			score = fmt.Sprintf("%d", row.ProduceScore.Score)
			scoreDate = row.ProduceScore.Date.Format("2006-01-02")
		}

		_, _ = fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
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
