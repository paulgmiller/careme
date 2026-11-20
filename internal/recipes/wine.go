package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/samber/lo"

	"careme/internal/ai"
	"careme/internal/cache"
)

const wineRecommendationsCachePrefix = "wine_recommendations/"

func recipeWineCacheKey(hash string) string {
	return wineRecommendationsCachePrefix + hash
}

func (rio recipeio) wineFromCache(ctx context.Context, hash string) (*ai.WineSelection, error) {
	wineReader, err := rio.Cache.Get(ctx, recipeWineCacheKey(hash))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = wineReader.Close()
	}()
	var selection ai.WineSelection
	err = json.NewDecoder(wineReader).Decode(&selection)
	return &selection, err
}

func (rio recipeio) wineExists(ctx context.Context, hash string) (bool, error) {
	return rio.Cache.Exists(ctx, recipeWineCacheKey(hash))
}

func (rio recipeio) saveWine(ctx context.Context, hash string, selection *ai.WineSelection) error {
	if selection == nil {
		return fmt.Errorf("wine selection is required")
	}
	body, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	return rio.Cache.Put(ctx, recipeWineCacheKey(hash), string(body), cache.Unconditional())
}

// waitForWine polls shared storage, so a pick may finish on any server.
// A saved selection with no wines is complete; only a missing record is retried.
// Recipes without wine styles need no pick when there is no saved selection.
func (rio recipeio) waitForWine(ctx context.Context, recipe ai.Recipe) (*ai.WineSelection, error) {
	hash := recipe.ComputeHash()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for wine pick %q: %w", hash, err)
		}
		selection, err := rio.wineFromCache(ctx, hash)
		if err == nil {
			return selection, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			return nil, fmt.Errorf("load wine pick %q: %w", hash, err)
		}
		if len(normalizedWineStyles(recipe.WineStyles)) == 0 {
			return &ai.WineSelection{}, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for wine pick %q: %w", hash, ctx.Err())
		case <-ticker.C:
		}
	}
}

func pickWine(ctx context.Context, client aiClient, staples staplesService, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error) {
	ctx, span := tracer.Start(ctx, "recipes.pickawine")
	defer span.End()
	var styles []string
	for _, style := range recipe.WineStyles {
		style = strings.TrimSpace(style)
		if style != "" {
			styles = append(styles, style)
		}
	}

	if len(styles) == 0 {
		return &ai.WineSelection{Commentary: "no wines styles for recipe", Wines: []ai.Ingredient{}}, nil
	}

	wines, err := staples.FetchWines(ctx, location, styles, date)
	if err != nil {
		return nil, err
	}

	if len(wines) == 0 {
		return &ai.WineSelection{Commentary: "no wines found", Wines: []ai.Ingredient{}}, nil
	}
	wines = lo.UniqBy(wines, func(i ai.InputIngredient) string {
		return i.ProductID
	})

	selection, err := client.PickWine(ctx, recipe, wines)
	if err != nil {
		return nil, err
	}
	enrichIngredientsMetadata(selection.Wines, inputIngredientMap(wines))
	return selection, nil
}

type recipeWineGenerator struct {
	recipeio
	client  aiClient
	staples staplesService
}

func NewRecipeWineGenerator(c cache.Cache, client aiClient, staples staplesService) *recipeWineGenerator {
	return &recipeWineGenerator{IO(c), client, staples}
}

func (w *recipeWineGenerator) RecipeReady(ctx context.Context, recipe ai.Recipe, locationID string, date time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	hash := recipe.ComputeHash()
	exists, err := w.wineExists(ctx, hash)
	if err != nil {
		return fmt.Errorf("check wine cache: %w", err)
	}
	if exists {
		return nil
	}

	selection, err := pickWine(ctx, w.client, w.staples, locationID, recipe, date)
	if err != nil {
		slog.ErrorContext(ctx, "failed to pick optional wine", "recipe_hash", hash, "error", err)
		selection = &ai.WineSelection{Wines: []ai.Ingredient{}}
	}
	if err := w.saveWine(ctx, hash, selection); err != nil {
		return fmt.Errorf("save wine selection: %w", err)
	}
	return nil
}

type mockWineGenerator struct{ recipeio }

func NewMockWineGenerator(c cache.Cache) *mockWineGenerator { return &mockWineGenerator{IO(c)} }
func (w *mockWineGenerator) RecipeReady(ctx context.Context, recipe ai.Recipe, location string, date time.Time) error {
	selection, err := (mock{}).pickAWine(ctx, location, recipe, date)
	if err != nil {
		return err
	}
	return w.saveWine(ctx, recipe.ComputeHash(), selection)
}
