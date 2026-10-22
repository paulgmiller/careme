package recipes

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/ingredients/cachekey"
	"careme/internal/locations"
	"careme/internal/providers/mnfoodclub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveParams_IsAtomic(t *testing.T) {
	t.Parallel()
	tmpDir, err := os.MkdirTemp("", "careme-test-saveparams-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Fatalf("failed to remove temp dir: %v", err)
		}
	})

	rio := IO(cache.NewFileCache(tmpDir))
	p := DefaultParams(&locations.Location{ID: "123", Name: "Test Store"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))

	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			errs <- rio.SaveParams(t.Context(), p)
		})
	}
	wg.Wait()
	close(errs)

	var ok, alreadyExists, other int
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrAlreadyExists):
			alreadyExists++
		default:
			other++
		}
	}

	if ok != 1 || other != 0 || alreadyExists != n-1 {
		t.Fatalf("expected 1 success + %d ErrAlreadyExists, got ok=%d alreadyExists=%d other=%d", n-1, ok, alreadyExists, other)
	}
}

func TestSaveParams_UsesPrefixedKey(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	p := DefaultParams(&locations.Location{ID: "123", Name: "Test Store"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))
	if err := rio.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, paramsCachePrefix, p.Hash())); err != nil {
		t.Fatalf("expected params at prefixed key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, p.Hash()+".params")); !os.IsNotExist(err) {
		t.Fatalf("did not expect legacy params key to be written; err=%v", err)
	}
}

func TestSaveParams_PersistsPreviousMenuPlanResponse(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	p := DefaultParams(&locations.Location{ID: "123", Name: "Test Store"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))
	p.PreviousMenuPlanResponseID = "resp-menu-123"
	p.PreviousMenuPlanPromptCacheKey = "careme:store-day:v1:test"

	if err := rio.SaveParams(t.Context(), p); err != nil {
		t.Fatalf("SaveParams failed: %v", err)
	}

	got, err := rio.ParamsFromCache(t.Context(), p.Hash())
	if err != nil {
		t.Fatalf("ParamsFromCache failed: %v", err)
	}
	if got.PreviousMenuPlanResponseID != p.PreviousMenuPlanResponseID {
		t.Fatalf("expected previous menu plan response id %q, got %q", p.PreviousMenuPlanResponseID, got.PreviousMenuPlanResponseID)
	}
	if got.PreviousMenuPlanPromptCacheKey != p.PreviousMenuPlanPromptCacheKey {
		t.Fatalf("expected previous menu plan cache key %q, got %q", p.PreviousMenuPlanPromptCacheKey, got.PreviousMenuPlanPromptCacheKey)
	}
}

func TestSaveShoppingList_UsesPrefixedKey(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	hash := "test-hash"
	list := &ai.ShoppingList{
		Recipes: []ai.Recipe{
			{
				OriginHash:   hash,
				ResponseID:   "resp-123",
				Title:        "One Pan Chicken",
				Description:  "Simple weeknight meal",
				Ingredients:  []ai.Ingredient{{Name: "Chicken", Quantity: "1 lb", Price: "5.99"}},
				Instructions: []string{"Prep ingredients", "Cook chicken"},
				Health:       "Balanced",
				DrinkPairing: "Chardonnay",
			},
		},
		Plan: &ai.MenuPlan{ResponseID: "resp-menu-123"},
	}

	if err := rio.SaveShoppingList(t.Context(), list, hash); err != nil {
		t.Fatalf("SaveShoppingList failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, ShoppingListCachePrefix, hash)); err != nil {
		t.Fatalf("expected shopping list at prefixed key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, hash)); !os.IsNotExist(err) {
		t.Fatalf("did not expect legacy root shopping list key to be written; err=%v", err)
	}

	got, err := rio.FromCache(t.Context(), hash)
	if err != nil {
		t.Fatalf("FromCache failed: %v", err)
	}
	if got.Recipes[0].ResponseID != list.Recipes[0].ResponseID {
		t.Fatalf("expected recipe response id %q, got %q", list.Recipes[0].ResponseID, got.Recipes[0].ResponseID)
	}
	if got.Plan == nil || got.Plan.ResponseID != list.Plan.ResponseID {
		t.Fatalf("expected menu plan response id %q, got %+v", list.Plan.ResponseID, got.Plan)
	}
}

func TestSaveIngredients_UsesPrefixedKey(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	hash := "ingredient-hash"
	ingredients := []ai.InputIngredient{
		{
			ProductID:   "chicken-1",
			VariantID:   "variant-42",
			Description: "Chicken Breast",
			Size:        "1 lb",
		},
	}

	if err := rio.SaveIngredients(t.Context(), hash, ingredients); err != nil {
		t.Fatalf("SaveIngredients failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, ingredientsCachePrefix, hash)); err != nil {
		t.Fatalf("expected ingredients at prefixed key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, hash)); !os.IsNotExist(err) {
		t.Fatalf("did not expect legacy root ingredients key to be written; err=%v", err)
	}

	got, err := rio.IngredientsFromCache(t.Context(), hash)
	if err != nil {
		t.Fatalf("IngredientsFromCache failed: %v", err)
	}
	if len(got) != 1 || got[0].Description != "Chicken Breast" || got[0].ProductID != "chicken-1" || got[0].VariantID != "variant-42" {
		t.Fatalf("unexpected ingredients payload: %+v", got)
	}
}

func TestSaveIngredients_PreservesGrade(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	hash := "ingredient-input-hash"
	ingredients := []ai.InputIngredient{
		{
			ProductID:   "chicken-1",
			Description: "Chicken Breast",
			Size:        "1 lb",
			Grade:       &ai.IngredientGrade{Score: 9, Reason: "Fresh and flexible."},
		},
	}

	if err := rio.SaveIngredients(t.Context(), hash, ingredients); err != nil {
		t.Fatalf("SaveIngredients failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, ingredientsCachePrefix, hash)); err != nil {
		t.Fatalf("expected ingredients at prefixed key: %v", err)
	}

	got, err := rio.IngredientsFromCache(t.Context(), hash)
	if err != nil {
		t.Fatalf("IngredientsFromCache failed: %v", err)
	}
	if len(got) != 1 || got[0].Description != "Chicken Breast" || got[0].Grade == nil || got[0].Grade.Score != 9 {
		t.Fatalf("unexpected input ingredients payload: %+v", got)
	}
}

func TestSaveWine_UsesNonConflictingPrefixWhenRecipeKeyAlreadyExists(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	hash := "recipe-hash"
	if err := cacheStore.Put(t.Context(), recipeCachePrefix+hash, `{"title":"Roast Chicken"}`, cache.Unconditional()); err != nil {
		t.Fatalf("failed to seed recipe entry: %v", err)
	}

	if err := rio.SaveWine(t.Context(), hash, &ai.WineSelection{
		Commentary: "Try a tempranillo.",
		Wines:      []ai.Ingredient{{Name: "Tempranillo", Quantity: "750mL", Price: "$12.99"}},
	}); err != nil {
		t.Fatal("shouldn't matter if theres a conflicting recipe key", "error", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, wineRecommendationsCachePrefix, hash)); err != nil {
		t.Fatalf("expected wine recommendation at prefixed key: %v", err)
	}

	got, err := rio.WineFromCache(t.Context(), hash)
	if err != nil {
		t.Fatalf("WineFromCache failed: %v", err)
	}
	if got.Commentary != "Try a tempranillo." {
		t.Fatalf("unexpected cached wine recommendation: got %q", got)
	}
}

func TestParamsDateHashVersionsRoundTrip(t *testing.T) {
	t.Parallel()
	zone, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	for _, id := range []string{"70500874", "mnfoodclub_delivery"} {
		for _, day := range []int{19, 20} {
			for _, version := range []int{0, 1} {
				t.Run(fmt.Sprintf("%s/day%d/version%d", id, day, version), func(t *testing.T) {
					date := time.Date(2026, 10, day, 0, 0, 0, 0, zone)
					p := &generatorParams{
						Location: &locations.Location{ID: id}, Date: date, DateHashVersion: version,
						Instructions: "vegetarian", Directive: "seasonal",
						Saved: []ai.Recipe{{Title: "Saved recipe"}}, Dismissed: []ai.Recipe{{Title: "Dismissed recipe"}},
					}
					var expected hash.Hash
					if version == 0 {
						expected = cachekey.HashForLegacyStoreDate(id, date)
						if id == "mnfoodclub_delivery" {
							expected = cachekey.NewHash(id, date, mnfoodclub.NewIdentityProvider().Signature())
						}
					} else {
						expected = cachekey.HashForStore(id, date)
					}
					locationHash := base64.RawURLEncoding.EncodeToString(expected.Sum(nil))
					for _, input := range []string{p.Instructions, p.Directive, "saved" + p.Saved[0].ComputeHash(), "dismissed" + p.Dismissed[0].ComputeHash()} {
						_, err := io.WriteString(expected, input)
						require.NoError(t, err)
					}
					expectedHash := base64.RawURLEncoding.EncodeToString(expected.Sum(nil))
					rio := IO(cache.NewFileCache(t.TempDir()))
					require.NoError(t, rio.SaveParams(t.Context(), p))
					loaded, err := rio.ParamsFromCache(t.Context(), expectedHash)
					require.NoError(t, err)
					assert.True(t, date.Equal(loaded.Date))
					assert.Equal(t, date.Format(time.RFC3339Nano), loaded.Date.Format(time.RFC3339Nano))
					assert.Equal(t, version, loaded.DateHashVersion)
					assert.Equal(t, expectedHash, loaded.Hash())
					assert.Equal(t, locationHash, loaded.LocationHash())
					blob, err := json.Marshal(loaded)
					require.NoError(t, err)
					if version == 0 {
						assert.NotContains(t, string(blob), "date_hash_version")
					} else {
						assert.Contains(t, string(blob), `"date_hash_version":1`)
					}
					loaded.Directive = "make it spicy"
					assert.NotEqual(t, expectedHash, loaded.Hash())
					assert.Equal(t, locationHash, loaded.LocationHash())
					require.NoError(t, rio.SaveParams(t.Context(), loaded))
					regenerated, err := rio.ParamsFromCache(t.Context(), loaded.Hash())
					require.NoError(t, err)
					assert.Equal(t, version, regenerated.DateHashVersion)
					assert.Equal(t, loaded.Hash(), regenerated.Hash())
				})
			}
		}
	}
}
