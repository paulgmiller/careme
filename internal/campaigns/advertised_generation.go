package campaigns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/prompts"
	"careme/internal/routing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type advertisedRecipeIO interface {
	SaveParams(ctx context.Context, p *recipes.GeneratorParams) error
	SaveRecipe(ctx context.Context, recipe ai.Recipe) error
	SaveShoppingList(ctx context.Context, shoppingList *ai.ShoppingList, hash string) error
}

type advertisedLocationStore interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type waiter interface {
	Wait()
}

type AdvertisedRecipeGenerator struct {
	locations advertisedLocationStore
	generator recipes.ExtGenerator
	rio       advertisedRecipeIO
	cache     cache.Cache
}

func NewAdvertisedRecipeGenerator(
	locations advertisedLocationStore,
	generator recipes.ExtGenerator,
	rio advertisedRecipeIO,
	cache cache.Cache,
) *AdvertisedRecipeGenerator {
	return &AdvertisedRecipeGenerator{
		locations: locations,
		generator: generator,
		rio:       rio,
		cache:     cache,
	}
}

func RunAdvertisedRecipeGeneration(ctx context.Context, cfg *config.Config) error {
	cacheStore, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}

	centroids := locations.LoadCentroids()
	locationStorage, err := locations.New(cfg, cacheStore, centroids)
	if err != nil {
		return fmt.Errorf("create location storage: %w", err)
	}

	rio := recipes.IO(cacheStore)
	generator, wait, err := newAdvertisedRecipeGenerator(cfg, cacheStore, rio)
	if err != nil {
		return err
	}
	if wait != nil {
		defer wait.Wait()
	}

	_, err = NewAdvertisedRecipeGenerator(locationStorage, generator, rio, cacheStore).Generate(ctx)
	return err
}

func (g *AdvertisedRecipeGenerator) Register(mux routing.Registrar) {
	mux.HandleFunc("POST /campaigns/advertised-recipes/generate", g.handleGenerate)
}

func (g *AdvertisedRecipeGenerator) handleGenerate(w http.ResponseWriter, r *http.Request) {
	manifest, err := g.Generate(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to generate advertised recipe manifest", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(manifest); err != nil {
		slog.ErrorContext(r.Context(), "failed to write advertised recipe manifest response", "error", err)
	}
}

func (g *AdvertisedRecipeGenerator) Generate(ctx context.Context) (*AdvertisedRecipeManifest, error) {
	manifest := AdvertisedRecipeManifest{
		GeneratedAt: time.Now(),
	}
	for _, advertised := range AdvertisedRecipeLocations() {
		entry, err := g.generateLocation(ctx, advertised.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to generate advertised recipes", "location", advertised.ID, "error", err)
			manifest.Failures = append(manifest.Failures, AdvertisedRecipeFailure{
				LocationID: advertised.ID,
				Error:      err.Error(),
			})
			continue
		}
		manifest.Entries = append(manifest.Entries, entry)
	}

	if len(manifest.Entries) == 0 {
		return nil, fmt.Errorf("generated zero advertised recipe entries")
	}
	if err := SaveAdvertisedRecipeManifest(ctx, g.cache, manifest); err != nil {
		return nil, fmt.Errorf("save advertised recipe manifest: %w", err)
	}

	slog.InfoContext(ctx, "generated advertised recipe manifest", "entries", len(manifest.Entries), "failures", len(manifest.Failures))
	return &manifest, nil
}

func (g *AdvertisedRecipeGenerator) generateLocation(ctx context.Context, locationID string) (AdvertisedRecipeEntry, error) {
	loc, err := g.locations.GetLocationByID(ctx, locationID)
	if err != nil {
		return AdvertisedRecipeEntry{}, fmt.Errorf("hydrate location: %w", err)
	}

	date, err := recipes.StoreToDate(ctx, time.Now(), loc)
	if err != nil {
		return AdvertisedRecipeEntry{}, fmt.Errorf("resolve store date: %w", err)
	}

	params := recipes.DefaultParams(loc, date)
	hash := params.Hash()
	if err := g.rio.SaveParams(ctx, params); err != nil && !errors.Is(err, recipes.ErrAlreadyExists) {
		return AdvertisedRecipeEntry{}, fmt.Errorf("save params: %w", err)
	}

	shoppingList, err := g.generator.GenerateRecipes(ctx, params)
	if err != nil {
		return AdvertisedRecipeEntry{}, fmt.Errorf("generate recipes: %w", err)
	}
	if err := g.rio.SaveShoppingList(ctx, shoppingList, hash); err != nil {
		return AdvertisedRecipeEntry{}, fmt.Errorf("save shopping list: %w", err)
	}

	recipeHashes := make([]string, 0, len(shoppingList.Recipes))
	for _, recipe := range shoppingList.Recipes {
		recipeHashes = append(recipeHashes, recipe.ComputeHash())
	}

	return AdvertisedRecipeEntry{
		Location:         *loc,
		Date:             params.Date,
		ShoppingListHash: hash,
		RecipeHashes:     recipeHashes,
		GeneratedAt:      time.Now(),
	}, nil
}

func newAdvertisedRecipeGenerator(cfg *config.Config, cacheStore cache.ListCache, rio advertisedRecipeIO) (recipes.ExtGenerator, waiter, error) {
	if cfg.Mocks.Enable {
		critiquer := critique.NewMock(cacheStore)
		return recipes.NewMockGenerator(rio, critiquer), nil, nil
	}

	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	grader := ingredientgrading.NewManager(cfg, cacheStore, httpClient)
	critiquer := critique.NewManager(cfg, cacheStore, httpClient)
	staples, err := recipes.NewCachedStaplesService(cfg, cacheStore, grader)
	if err != nil {
		return nil, nil, fmt.Errorf("create staples service: %w", err)
	}
	aiclient := ai.NewClient(cfg.AI.APIKey, "TODOMODEL", httpClient, prompts.NewCacheRecorder(cacheStore))
	generator, err := recipes.NewGenerator(aiclient, critiquer, staples, recipes.StatusStore(cacheStore), rio)
	if err != nil {
		return nil, nil, fmt.Errorf("create generator: %w", err)
	}
	return generator, critiquer, nil
}
