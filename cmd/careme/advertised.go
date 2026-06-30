package main

import (
	"context"
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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type advertisedRecipeIO interface {
	SaveParams(ctx context.Context, p *recipes.GeneratorParams) error
	SaveRecipe(ctx context.Context, recipe ai.Recipe) error
	SaveShoppingList(ctx context.Context, shoppingList *ai.ShoppingList, hash string) error
}

func runAdvertisedRecipeGeneration(ctx context.Context, cfg *config.Config) error {
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

	manifest := recipes.AdvertisedRecipeManifest{
		GeneratedAt: time.Now(),
	}
	for _, advertised := range recipes.AdvertisedRecipeLocations() {
		entry, err := generateAdvertisedLocation(ctx, locationStorage, generator, rio, advertised.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to generate advertised recipes", "location", advertised.ID, "error", err)
			manifest.Failures = append(manifest.Failures, recipes.AdvertisedRecipeFailure{
				LocationID: advertised.ID,
				Error:      err.Error(),
			})
			continue
		}
		manifest.Entries = append(manifest.Entries, entry)
	}

	if len(manifest.Entries) == 0 {
		return fmt.Errorf("generated zero advertised recipe entries")
	}
	if err := recipes.SaveAdvertisedRecipeManifest(ctx, cacheStore, manifest); err != nil {
		return fmt.Errorf("save advertised recipe manifest: %w", err)
	}

	slog.InfoContext(ctx, "generated advertised recipe manifest", "entries", len(manifest.Entries), "failures", len(manifest.Failures))
	return nil
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

type advertisedLocationStore interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

func generateAdvertisedLocation(
	ctx context.Context,
	locationStorage advertisedLocationStore,
	generator recipes.ExtGenerator,
	rio advertisedRecipeIO,
	locationID string,
) (recipes.AdvertisedRecipeEntry, error) {
	loc, err := locationStorage.GetLocationByID(ctx, locationID)
	if err != nil {
		return recipes.AdvertisedRecipeEntry{}, fmt.Errorf("hydrate location: %w", err)
	}

	date, err := recipes.StoreToDate(ctx, time.Now(), loc)
	if err != nil {
		return recipes.AdvertisedRecipeEntry{}, fmt.Errorf("resolve store date: %w", err)
	}

	params := recipes.DefaultParams(loc, date)
	hash := params.Hash()
	if err := rio.SaveParams(ctx, params); err != nil && !errors.Is(err, recipes.ErrAlreadyExists) {
		return recipes.AdvertisedRecipeEntry{}, fmt.Errorf("save params: %w", err)
	}

	shoppingList, err := generator.GenerateRecipes(ctx, params)
	if err != nil {
		return recipes.AdvertisedRecipeEntry{}, fmt.Errorf("generate recipes: %w", err)
	}
	if err := rio.SaveShoppingList(ctx, shoppingList, hash); err != nil {
		return recipes.AdvertisedRecipeEntry{}, fmt.Errorf("save shopping list: %w", err)
	}

	recipeHashes := make([]string, 0, len(shoppingList.Recipes))
	for _, recipe := range shoppingList.Recipes {
		recipeHashes = append(recipeHashes, recipe.ComputeHash())
	}

	return recipes.AdvertisedRecipeEntry{
		Location:         *loc,
		Date:             params.Date,
		ShoppingListHash: hash,
		RecipeHashes:     recipeHashes,
		GeneratedAt:      time.Now(),
	}, nil
}
