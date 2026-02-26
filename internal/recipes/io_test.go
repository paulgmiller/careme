package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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
	wg.Add(n)

	errs := make(chan error, n)
	for range n {
		go func() {
			defer wg.Done()
			errs <- rio.SaveParams(t.Context(), p)
		}()
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
		ConversationID: "conversation-123",
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
	if got.ConversationID != list.ConversationID {
		t.Fatalf("expected conversation id %q, got %q", list.ConversationID, got.ConversationID)
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

func TestFromCache_FallsBackToLegacyHashedKeyForCanonicalHash(t *testing.T) {
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	p := DefaultParams(&locations.Location{ID: "loc-123", Name: "Store"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))
	hash := p.Hash()
	legacyHash, ok := legacyRecipeHash(hash)
	if !ok {
		t.Fatal("expected to derive legacy recipe hash")
	}

	list := ai.ShoppingList{ConversationID: "legacy-conversation"}
	listJSON, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("failed to marshal shopping list: %v", err)
	}
	if err := cacheStore.Put(t.Context(), legacyHash, string(listJSON), cache.Unconditional()); err != nil {
		t.Fatalf("failed to store legacy shopping list key: %v", err)
	}

	got, err := rio.FromCache(t.Context(), hash)
	if err != nil {
		t.Fatalf("FromCache failed to read legacy hash key: %v", err)
	}
	if got.ConversationID != list.ConversationID {
		t.Fatalf("expected conversation id %q, got %q", list.ConversationID, got.ConversationID)
	}
}

func TestParamsFromCache_FallsBackToLegacyHashedKeyForCanonicalHash(t *testing.T) {
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	p := DefaultParams(&locations.Location{ID: "loc-321", Name: "Store"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))
	hash := p.Hash()
	legacyHash, ok := legacyRecipeHash(hash)
	if !ok {
		t.Fatal("expected to derive legacy recipe hash")
	}

	paramsJSON, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("failed to marshal params: %v", err)
	}
	if err := cacheStore.Put(t.Context(), legacyHash+".params", string(paramsJSON), cache.Unconditional()); err != nil {
		t.Fatalf("failed to store legacy params key: %v", err)
	}

	got, err := rio.ParamsFromCache(t.Context(), hash)
	if err != nil {
		t.Fatalf("ParamsFromCache failed to read legacy hash key: %v", err)
	}
	if got.Location == nil || got.Location.ID != p.Location.ID {
		t.Fatalf("expected location id %q, got %+v", p.Location.ID, got.Location)
	}
}

func TestIngredientsFromCache_FallsBackToLegacyHashedKeyForCanonicalHash(t *testing.T) {
	tmpDir := t.TempDir()
	cacheStore := cache.NewFileCache(tmpDir)
	rio := IO(cacheStore)

	p := DefaultParams(&locations.Location{ID: "loc-777", Name: "Store"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))
	hash := p.LocationHash()
	legacyHash, ok := legacyLocationHash(hash)
	if !ok {
		t.Fatal("expected to derive legacy location hash")
	}

	ingredients := []kroger.Ingredient{{Description: loPtr("Legacy Chicken")}}
	ingredientsJSON, err := json.Marshal(ingredients)
	if err != nil {
		t.Fatalf("failed to marshal ingredients: %v", err)
	}
	if err := cacheStore.Put(t.Context(), legacyHash, string(ingredientsJSON), cache.Unconditional()); err != nil {
		t.Fatalf("failed to store legacy ingredients key: %v", err)
	}

	got, err := rio.IngredientsFromCache(t.Context(), hash)
	if err != nil {
		t.Fatalf("IngredientsFromCache failed to read legacy hash key: %v", err)
	}
	if len(got) != 1 || got[0].Description == nil || *got[0].Description != "Legacy Chicken" {
		t.Fatalf("unexpected ingredients payload: %+v", got)
	}
}

func loPtr(v string) *string {
	return &v
}
