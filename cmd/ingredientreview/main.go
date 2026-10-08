package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/ingredients/gradereview"
	"careme/internal/ingredients/grading"
	"careme/internal/locations"
	"careme/internal/providerregistry"
	"careme/internal/recipes"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("ingredientreview", flag.ContinueOnError)
	addr := fs.String("addr", ":8090", "address for the ingredient grade review app")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if !cfg.IngredientGrading.Enable {
		return fmt.Errorf("ingredient review requires ingredient grading to be enabled")
	}

	cacheStore, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}

	factory := providerregistry.NewFactory(cfg)
	centroids := locations.LoadCentroids()
	locationBackends, err := factory.NewLocationBackends(centroids)
	if err != nil {
		return fmt.Errorf("create location backends: %w", err)
	}
	locationStore, err := locations.New(cacheStore, centroids, locationBackends)
	if err != nil {
		return fmt.Errorf("create location storage: %w", err)
	}
	stapleBackends, err := factory.NewStaplesBackends()
	if err != nil {
		return fmt.Errorf("create staples backends: %w", err)
	}
	grader := grading.NewManager(cfg, cacheStore, http.DefaultClient)
	catalog := storeCatalog{locations: locationStore, staples: recipes.NewCachedStaplesService(stapleBackends, cacheStore, reviewGrader{grader}), now: time.Now}
	server := &http.Server{
		Addr:              *addr,
		Handler:           gradereview.NewHandler(cacheStore, grader.CacheVersion(), gradereview.Options{Catalog: catalog}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Ingredient grade review app listening at http://%s", *addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type locationLookup interface {
	GetLocationByID(context.Context, string) (*locations.Location, error)
}
type staplesFetcher interface {
	FetchStaples(context.Context, *recipes.GeneratorParams) ([]ai.InputIngredient, error)
}
type storeCatalog struct {
	locations locationLookup
	staples   staplesFetcher
	now       func() time.Time
}

func (c storeCatalog) LoadCatalog(ctx context.Context, id string) (*locations.Location, []ai.InputIngredient, error) {
	location, err := c.locations.GetLocationByID(ctx, id)
	if err != nil {
		return nil, nil, fmt.Errorf("load store %q: %w", id, err)
	}
	date, err := locations.StoreToDate(ctx, c.now(), location)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve store date: %w", err)
	}
	ingredients, err := c.staples.FetchStaples(ctx, &recipes.GeneratorParams{Location: location, Date: date})
	if err != nil {
		return nil, nil, fmt.Errorf("load store catalog %q: %w", id, err)
	}
	return location, ingredients, nil
}

type ingredientGrader interface {
	GradeIngredients(context.Context, []ai.InputIngredient) ([]ai.InputIngredient, error)
}

// Cached catalogs may contain grades from an older model. Resolve grades through
// the configured model's grade cache instead of trusting those catalog grades.
type reviewGrader struct{ grader ingredientGrader }

func (g reviewGrader) GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	inputs := append([]ai.InputIngredient(nil), ingredients...)
	for i := range inputs {
		inputs[i].Grade = nil
		inputs[i].Embedding = nil
	}
	return g.grader.GradeIngredients(ctx, inputs)
}
