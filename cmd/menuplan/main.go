package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/locations"
	"careme/internal/recipes"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type locationStore interface {
	GetLocationsByZip(ctx context.Context, zipcode string) ([]locations.Location, error)
	HasInventory(locationID string) bool
}

type menuPlanner interface {
	CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error)
}

type staplesService interface {
	FetchStaples(ctx context.Context, p *recipes.GeneratorParams) ([]ai.InputIngredient, error)
}

type planService struct {
	planner menuPlanner
	staples staplesService
}

type storeMenuPlan struct {
	Location locations.Location
	Date     time.Time
	Plan     *ai.MenuPlan
	Err      error
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	var zip string
	var limit int
	var instructions string

	fs := flag.NewFlagSet("menuplan", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&zip, "zip", "", "ZIP code to plan from")
	fs.IntVar(&limit, "stores", 5, "number of grocery stores to plan for")
	fs.StringVar(&instructions, "instructions", "", "extra cooking notes, like make it vegetarian")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if zip = strings.TrimSpace(zip); zip == "" {
		return errors.New("must provide -zip")
	}
	if limit < 1 {
		return errors.New("-stores must be greater than zero")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	cacheStore, err := newCache(cfg)
	if err != nil {
		return err
	}
	locationStore, err := locations.New(cfg, cacheStore, locations.LoadCentroids())
	if err != nil {
		return fmt.Errorf("create location store: %w", err)
	}
	service, err := newPlanService(cfg, cacheStore)
	if err != nil {
		return err
	}

	stores, err := firstInventoryStores(ctx, locationStore, zip, limit)
	if err != nil {
		return err
	}

	results := makeStoreMenuPlans(ctx, service, stores, instructions, time.Now())

	if err := writeMenuPlans(out, zip, results); err != nil {
		return err
	}

	var failures int
	for _, result := range results {
		if result.Err != nil {
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("failed to create %d of %d menu plans", failures, len(results))
	}
	return nil
}

func newCache(cfg *config.Config) (cache.ListCache, error) {
	if cfg.Mocks.Enable {
		return cache.NewFileCache("recipes"), nil
	}
	cacheStore, err := cache.MakeCache()
	if err != nil {
		return nil, fmt.Errorf("create cache: %w", err)
	}
	return cacheStore, nil
}

func newPlanService(cfg *config.Config, cacheStore cache.ListCache) (planService, error) {
	if cfg.Mocks.Enable {
		return planService{
			planner: mockMenuPlanner{},
			staples: mockStaplesService{},
		}, nil
	}

	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	grader := ingredientgrading.NewManager(cfg, cacheStore, httpClient)
	staples, err := recipes.NewCachedStaplesService(cfg, cacheStore, grader)
	if err != nil {
		return planService{}, fmt.Errorf("create staples service: %w", err)
	}
	return planService{
		planner: ai.NewClient(cfg.AI.APIKey, "TODOMODEL", httpClient),
		staples: staples,
	}, nil
}

func makeMenuPlan(ctx context.Context, service planService, store locations.Location, date time.Time, instructions string, count int) (*ai.MenuPlan, error) {
	params := recipes.DefaultParams(&store, date)
	params.Instructions = instructions

	ingredients, err := service.staples.FetchStaples(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("fetch staples: %w", err)
	}
	ingredients = filterMenuIngredients(ingredients)

	plan, err := service.planner.CreateMenuPlan(ctx, &store, ingredients, compactStrings(params.Instructions), date, nil, count)
	if err != nil {
		return nil, fmt.Errorf("create menu plan: %w", err)
	}
	return plan, nil
}

func makeStoreMenuPlans(ctx context.Context, service planService, stores []locations.Location, instructions string, now time.Time) []storeMenuPlan {
	results := make([]storeMenuPlan, len(stores))
	var wg sync.WaitGroup
	wg.Add(len(stores))
	for i, store := range stores {
		i, store := i, store
		go func() {
			defer wg.Done()
			date, err := recipes.StoreToDate(ctx, now, &store)
			if err != nil {
				results[i] = storeMenuPlan{Location: store, Err: err}
				return
			}

			plan, err := makeMenuPlan(ctx, service, store, date, instructions, 3)
			results[i] = storeMenuPlan{
				Location: store,
				Date:     date,
				Plan:     plan,
				Err:      err,
			}
		}()
	}
	wg.Wait()
	return results
}

func filterMenuIngredients(ingredients []ai.InputIngredient) []ai.InputIngredient {
	filtered := make([]ai.InputIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		if ingredient.Grade != nil && ingredient.Grade.Score <= 6 {
			continue
		}
		filtered = append(filtered, ingredient)
	}
	return filtered
}

type mockStaplesService struct{}

func (mockStaplesService) FetchStaples(context.Context, *recipes.GeneratorParams) ([]ai.InputIngredient, error) {
	return []ai.InputIngredient{
		{ProductID: "mock-chicken", Description: "chicken thighs"},
		{ProductID: "mock-beans", Description: "black beans"},
		{ProductID: "mock-greens", Description: "seasonal greens"},
	}, nil
}

type mockMenuPlanner struct{}

func (mockMenuPlanner) CreateMenuPlan(context.Context, *locations.Location, []ai.InputIngredient, []string, time.Time, []string, int) (*ai.MenuPlan, error) {
	return &ai.MenuPlan{Plans: []ai.RecipePlan{
		{Cuisine: "Korean", AnchorIngredient: "chicken thighs", Technique: "sheet pan"},
		{Cuisine: "Mexican", AnchorIngredient: "black beans", Technique: "quick simmer"},
		{Cuisine: "Mediterranean", AnchorIngredient: "seasonal greens", Technique: "grain bowl", Fancy: true},
	}}, nil
}

func firstInventoryStores(ctx context.Context, store locationStore, zip string, limit int) ([]locations.Location, error) {
	found, err := store.GetLocationsByZip(ctx, zip)
	if err != nil {
		return nil, fmt.Errorf("find stores for zip %s: %w", zip, err)
	}

	stores := make([]locations.Location, 0, limit)
	for _, loc := range found {
		if !store.HasInventory(loc.ID) {
			slog.InfoContext(ctx, "skipping store without inventory support", "location_id", loc.ID, "name", loc.Name)
			continue
		}
		stores = append(stores, loc)
		if len(stores) == limit {
			break
		}
	}
	if len(stores) == 0 {
		return nil, fmt.Errorf("no inventory-backed grocery stores found for zip %s", zip)
	}
	return stores, nil
}

func writeMenuPlans(w io.Writer, zip string, results []storeMenuPlan) error {
	if _, err := fmt.Fprintf(w, "Menu plans for %s\n", zip); err != nil {
		return err
	}
	for i, result := range results {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := writeStoreMenuPlan(w, i+1, result); err != nil {
			return err
		}
	}
	return nil
}

func writeStoreMenuPlan(w io.Writer, number int, result storeMenuPlan) error {
	loc := result.Location
	if _, err := fmt.Fprintf(w, "%d. %s\n", number, displayStoreName(loc)); err != nil {
		return err
	}
	for _, line := range storeDetailLines(loc) {
		if _, err := fmt.Fprintf(w, "   %s\n", line); err != nil {
			return err
		}
	}
	if !result.Date.IsZero() {
		if _, err := fmt.Fprintf(w, "   Date: %s\n", result.Date.Format("2006-01-02")); err != nil {
			return err
		}
	}
	if result.Err != nil {
		_, err := fmt.Fprintf(w, "   Could not make a menu plan: %v\n", result.Err)
		return err
	}
	if result.Plan == nil {
		_, err := fmt.Fprintln(w, "   No menu plan returned.")
		return err
	}
	if len(result.Plan.Plans) == 0 {
		_, err := fmt.Fprintln(w, "   No menu plan ideas returned.")
		return err
	}

	if _, err := fmt.Fprintln(w, "   Plan:"); err != nil {
		return err
	}
	for i, plan := range result.Plan.Plans {
		fancy := ""
		if plan.Fancy {
			fancy = " (fancier)"
		}
		if _, err := fmt.Fprintf(w, "   - %d: %s with %s, %s%s\n", i+1, plan.Cuisine, plan.AnchorIngredient, plan.Technique, fancy); err != nil {
			return err
		}
	}
	return nil
}

func displayStoreName(loc locations.Location) string {
	parts := make([]string, 0, 2)
	if chain := strings.TrimSpace(loc.Chain); chain != "" {
		parts = append(parts, chain)
	}
	if name := strings.TrimSpace(loc.Name); name != "" {
		parts = append(parts, name)
	}
	if len(parts) == 0 {
		return loc.ID
	}
	return strings.Join(parts, " - ")
}

func storeDetailLines(loc locations.Location) []string {
	var lines []string
	if loc.ID != "" {
		lines = append(lines, "Store ID: "+loc.ID)
	}
	addressParts := compactStrings(loc.Address, loc.State, loc.ZipCode)
	if len(addressParts) > 0 {
		lines = append(lines, "Address: "+strings.Join(addressParts, ", "))
	}
	return lines
}

func compactStrings(values ...string) []string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return parts
}
