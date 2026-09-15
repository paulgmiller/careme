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
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/locations/geo"
	"careme/internal/parallelism"
	"careme/internal/recipes"
	"careme/internal/recipes/prompts"

	"github.com/samber/lo"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type locationStore interface {
	GetLocationsByCoordinates(ctx context.Context, coordinates geo.Coordinate) ([]locations.Location, error)
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
	HasInventory(locationID string) bool
}

type menuPlanner interface {
	CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error)
}

type staplesService interface {
	FetchStaples(ctx context.Context, p *recipes.GeneratorParams) ([]ai.InputIngredient, error)
}

type pantryService interface {
	FetchPantry(ctx context.Context, p *recipes.GeneratorParams) ([]ai.InputIngredient, error)
}

type ingredientEmbedder interface {
	EmbedIngredients(context.Context, []string) ([]ai.IngredientEmbedding, error)
}

type planService struct {
	planner  menuPlanner
	staples  staplesService
	pantry   pantryService
	embedder ingredientEmbedder
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
	var location string
	var plans int
	var limit int
	var instructions string

	fs := flag.NewFlagSet("menuplan", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&zip, "zip", "", "ZIP code to plan from")
	fs.StringVar(&location, "location", "", "store location ID (mutually exclusive with -zip)")
	fs.IntVar(&plans, "plans", 10, "number of menus per location, each containing 3 recipe ideas")
	fs.IntVar(&limit, "stores", 5, "number of grocery stores to plan for")
	fs.StringVar(&instructions, "instructions", "", "extra cooking notes, like make it vegetarian")
	if err := fs.Parse(args); err != nil {
		return err
	}
	zip, location = strings.TrimSpace(zip), strings.TrimSpace(location)
	if (zip == "") == (location == "") {
		return errors.New("provide exactly one of -location or -zip")
	}
	if plans < 1 {
		return errors.New("-plans must be greater than zero")
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
	centroids := locations.LoadCentroids()
	locationStore, err := locations.New(cfg, cacheStore, centroids)
	if err != nil {
		return fmt.Errorf("create location store: %w", err)
	}
	service, err := newPlanService(cfg, cacheStore)
	if err != nil {
		return err
	}

	stores, err := selectStores(ctx, locationStore, location, zip, limit)
	if err != nil {
		return err
	}

	results := makeStoreMenuPlans(ctx, service, stores, instructions, time.Now(), plans)
	printHistogram(results, func(r ai.RecipePlan, _ int) string {
		return r.SideVegetable
	})
	return nil
}

func printHistogram(respices []ai.RecipePlan, transform func(ai.RecipePlan, int) string) {
	histogram := lo.CountValues(lo.Map(respices, transform))
	keys := lo.Keys(histogram)
	slices.Sort(keys)
	for _, key := range keys {
		count := histogram[key]

		// Use strings.Repeat to dynamically build the horizontal bar
		bar := strings.Repeat("■", count)

		// %-10s left-aligns the key text with a 10-character padded margin
		fmt.Printf("%-10s (%d) %s\n", key, count, bar)
	}
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
			pantry:  mockPantryService{},
		}, nil
	}

	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	grader := ingredientgrading.NewEnrichingGrader(cfg, cacheStore, httpClient)
	staples, err := recipes.NewCachedStaplesService(cfg, cacheStore, grader)
	if err != nil {
		return planService{}, fmt.Errorf("create staples service: %w", err)
	}
	return planService{
		planner:  ai.NewClient(cfg.AI, httpClient, prompts.NewCacheRecorder(cacheStore)),
		staples:  staples,
		pantry:   staples,
		embedder: ai.NewIngredientEmbedder(cfg.AI.APIKey, httpClient),
	}, nil
}

func makeMenuPlans(ctx context.Context, service planService, store locations.Location, date time.Time, instructions string, count, plans int) ([]ai.RecipePlan, error) {
	params := recipes.DefaultParams(&store, date)
	params.Instructions = instructions

	ingredients, err := service.staples.FetchStaples(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("fetch staples: %w", err)
	}
	ingredients = filterMenuIngredients(ingredients)
	pantry, err := service.pantry.FetchPantry(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("fetch pantry: %w", err)
	}
	pantry = lo.Filter(pantry, func(ingredient ai.InputIngredient, _ int) bool {
		return ingredient.Grade == nil || ingredient.Grade.Score >= 5
	})

	return parallelism.Flatten(lo.Range(plans), func(int) ([]ai.RecipePlan, error) {
		plan, err := service.planner.CreateMenuPlan(ctx, &store, ingredients, compactStrings(params.Instructions), date, nil, count)
		if err != nil {
			return nil, fmt.Errorf("create menu plan: %w", err)
		}
		for i := range plan.Plans {
			if len(pantry) == 0 || service.embedder == nil {
				continue
			}
			query := pantryQuery(plan.Plans[i], ingredients)
			vectors, err := service.embedder.EmbedIngredients(ctx, []string{query})
			if err != nil {
				return nil, fmt.Errorf("embed pantry query %q: %w", query, err)
			}
			var output strings.Builder
			fmt.Fprintf(&output, "Pantry for %s\n", query)
			for _, category := range kroger.PantryCategories() {
				neighbors, err := pantryCategoryNeighbors(vectors[0], pantry, category)
				if err != nil {
					return nil, fmt.Errorf("find %s pantry items for %q: %w", category, query, err)
				}
				fmt.Fprintf(&output, "  %s\n %s\n", category, formatPantryNeighbors(neighbors))
			}
			fmt.Print(output.String())
		}
		return plan.Plans, nil
	})
}

func pantryCategoryNeighbors(query ai.IngredientEmbedding, pantry []ai.InputIngredient, category string) ([]ai.IngredientNeighbor, error) {
	candidates := lo.Filter(pantry, func(ingredient ai.InputIngredient, _ int) bool {
		return slices.Contains(ingredient.Categories, category)
	})
	return ai.NearestIngredients(query, candidates, 5)
}

func pantryQuery(plan ai.RecipePlan, ingredients []ai.InputIngredient) string {
	brands := make([]string, 0, len(ingredients))
	for _, ingredient := range ingredients {
		brand := strings.TrimSpace(strings.NewReplacer("®", "", "™", "", "℠", "").Replace(ingredient.Brand))
		if brand != "" {
			brands = append(brands, brand)
		}
	}
	// Remove longer names first, e.g. Simple Truth Organic before Simple Truth.
	slices.SortFunc(brands, func(a, b string) int { return len(b) - len(a) })
	clean := func(name string) string {
		name = strings.NewReplacer("®", "", "™", "", "℠", "").Replace(name)
		for _, brand := range brands {
			pattern := regexp.MustCompile(`(?i)(^|[^\p{L}\p{N}])` + regexp.QuoteMeta(brand) + `($|[^\p{L}\p{N}])`)
			name = pattern.ReplaceAllString(name, "${1}${2}")
		}
		return strings.Join(strings.Fields(name), " ")
	}
	return strings.Join(compactStrings(plan.Cuisine, clean(plan.AnchorIngredient), clean(plan.SideVegetable)), ", ")
}

func makeStoreMenuPlans(ctx context.Context, service planService, stores []locations.Location, instructions string, now time.Time, plans int) []ai.RecipePlan {
	results := make([][]ai.RecipePlan, len(stores))
	var wg sync.WaitGroup
	wg.Add(len(stores))
	for i, store := range stores {
		go func() {
			defer wg.Done()
			date, err := locations.StoreToDate(ctx, now, &store)
			if err != nil {
				slog.Warn("go error on store to date %s", "error", err)
				return
			}

			cuisines, err := makeMenuPlans(ctx, service, store, date, instructions, 3, plans)
			if err != nil {
				slog.Warn("go error %s", "error", err)
			}
			results[i] = cuisines
		}()
	}
	wg.Wait()
	return lo.Flatten(results)
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

type mockPantryService struct{}

func (mockPantryService) FetchPantry(context.Context, *recipes.GeneratorParams) ([]ai.InputIngredient, error) {
	return []ai.InputIngredient{{ProductID: "mock-cumin", Description: "ground cumin", Embedding: ai.IngredientEmbedding{1, 0}}}, nil
}

func formatPantryNeighbors(neighbors []ai.IngredientNeighbor) string {
	parts := make([]string, 0, len(neighbors))
	for _, neighbor := range neighbors {
		parts = append(parts, fmt.Sprintf("\t%.3f %s", neighbor.Similarity, neighbor.Ingredient.Description))
	}
	return strings.Join(parts, "\n")
}

type mockMenuPlanner struct{}

func (mockMenuPlanner) CreateMenuPlan(context.Context, *locations.Location, []ai.InputIngredient, []string, time.Time, []string, int) (*ai.MenuPlan, error) {
	return &ai.MenuPlan{Plans: []ai.RecipePlan{
		{Cuisine: "Korean", AnchorIngredient: "chicken thighs", DishFormat: "sheet pan", SideVegetable: "bok choy"},
		{Cuisine: "Mexican", AnchorIngredient: "black beans", DishFormat: "quick simmer", SideVegetable: "zucchini"},
		{Cuisine: "Mediterranean", AnchorIngredient: "seasonal greens", DishFormat: "grain bowl", SideVegetable: "eggplant", Fancy: true},
	}}, nil
}

func selectStores(ctx context.Context, store locationStore, location, zip string, limit int) ([]locations.Location, error) {
	if location != "" {
		loc, err := store.GetLocationByID(ctx, location)
		if err != nil {
			return nil, fmt.Errorf("find location %q: %w", location, err)
		}
		if !store.HasInventory(loc.ID) {
			return nil, fmt.Errorf("location %q has no inventory support", location)
		}
		return []locations.Location{*loc}, nil
	}
	coordinates, ok := locations.LoadCentroids().ZipCentroidByZIP(zip)
	if !ok {
		return nil, fmt.Errorf("coordinates not found for ZIP code %q", zip)
	}
	return firstInventoryStores(ctx, store, coordinates, limit)
}

func firstInventoryStores(ctx context.Context, store locationStore, coordinates geo.Coordinate, limit int) ([]locations.Location, error) {
	found, err := store.GetLocationsByCoordinates(ctx, coordinates)
	if err != nil {
		return nil, fmt.Errorf("find stores %w", err)
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
		return nil, fmt.Errorf("no inventory-backed grocery stores found")
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
		sideVegetable := ""
		if strings.TrimSpace(plan.SideVegetable) != "" {
			sideVegetable = fmt.Sprintf(", side veg: %s", plan.SideVegetable)
		}
		if _, err := fmt.Fprintf(w, "   - %d: %s with %s, %s%s%s\n", i+1, plan.Cuisine, plan.AnchorIngredient, plan.DishFormat, sideVegetable, fancy); err != nil {
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
