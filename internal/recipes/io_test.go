package recipes

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations"
)

func TestSaveParams_IsAtomic(t *testing.T) {
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

func TestSaveShoppingList_UsesPrefixedKey(t *testing.T) {
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	hash := "test-hash"
	list := &ai.ShoppingList{
		ResponseID: "resp-123",
		Recipes: []ai.Recipe{
			{
				Title:        "One Pan Chicken",
				Description:  "Simple weeknight meal",
				Ingredients:  []ai.Ingredient{{Name: "Chicken", Quantity: "1 lb", Price: "5.99"}},
				Instructions: []string{"Prep ingredients", "Cook chicken"},
				Health:       "Balanced",
				DrinkPairing: "Chardonnay",
			},
		},
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
	if got.ResponseID != list.ResponseID {
		t.Fatalf("expected response id %q, got %q", list.ResponseID, got.ResponseID)
	}
}

func TestSaveShoppingList_SavesDiscardedRecipesSeparately(t *testing.T) {
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	kept := ai.Recipe{
		Title:        "One Pan Chicken",
		Description:  "Simple weeknight meal",
		Ingredients:  []ai.Ingredient{{Name: "Chicken", Quantity: "1 lb", Price: "5.99"}},
		Instructions: []string{"Prep ingredients", "Cook chicken"},
		Health:       "Balanced",
		DrinkPairing: "Chardonnay",
	}
	discarded := ai.Recipe{
		Title:        "Mushy Pasta",
		Description:  "Too vague to keep",
		Ingredients:  []ai.Ingredient{{Name: "Pasta", Quantity: "1 lb", Price: "1.99"}},
		Instructions: []string{"Cook until done somehow"},
		Health:       "Heavy",
		DrinkPairing: "None",
	}
	hash := "test-hash"
	list := &ai.ShoppingList{
		ResponseID: "resp-123",
		Recipes:    []ai.Recipe{kept},
		Discarded:  []ai.Recipe{discarded},
	}

	if err := rio.SaveShoppingList(t.Context(), list, hash); err != nil {
		t.Fatalf("SaveShoppingList failed: %v", err)
	}

	if len(list.Discarded) != 0 {
		t.Fatalf("expected SaveShoppingList to clear discarded recipes after persisting, got %+v", list.Discarded)
	}

	stored, err := rio.SingleFromCache(t.Context(), discarded.ComputeHash())
	if err != nil {
		t.Fatalf("expected discarded recipe to be saved individually: %v", err)
	}
	if stored.Title != discarded.Title {
		t.Fatalf("expected discarded recipe title %q, got %q", discarded.Title, stored.Title)
	}
	if stored.OriginHash != hash {
		t.Fatalf("expected discarded recipe origin hash %q, got %q", hash, stored.OriginHash)
	}

	cachedList, err := rio.FromCache(t.Context(), hash)
	if err != nil {
		t.Fatalf("FromCache failed: %v", err)
	}
	if len(cachedList.Discarded) != 0 {
		t.Fatalf("expected cached shopping list to omit discarded recipes, got %+v", cachedList.Discarded)
	}
}

func TestSaveIngredients_UsesPrefixedKey(t *testing.T) {
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	hash := "ingredient-hash"
	ingredients := []kroger.Ingredient{
		{
			Description: loPtr("Chicken Breast"),
			Size:        loPtr("1 lb"),
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
	if len(got) != 1 || got[0].Description == nil || *got[0].Description != "Chicken Breast" {
		t.Fatalf("unexpected ingredients payload: %+v", got)
	}
}

func TestSaveWine_UsesNonConflictingPrefixWhenRecipeKeyAlreadyExists(t *testing.T) {
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

func loPtr(v string) *string {
	return &v
}
