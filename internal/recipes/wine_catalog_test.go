package recipes

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWineCandidateCatalogPreservesRecommendationAndEnrichesShoppingItem(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	store := cache.NewInMemoryCache()
	rio := IO(store)
	recipe := ai.Recipe{Title: "Dinner"}
	hash := recipe.ComputeHash()
	original := `{"commentary":"Old pairing.","wines":[{"id":"wine","variant_id":"legacy-upc","name":"Old red","quantity":"1 bottle","price":"$12.99"}]}`
	require.NoError(t, store.Put(t.Context(), recipeWineCacheKey(hash), original, cache.Unconditional()))
	recommendation, err := rio.wineFromCache(t.Context(), hash)
	require.NoError(t, err)
	var page bytes.Buffer
	require.NoError(t, templates.Recipe.ExecuteTemplate(&page, "recipe_wine", struct{ WineRecommendation *ai.WineSelection }{recommendation}))
	assert.Contains(t, page.String(), "Old red")
	assert.Contains(t, page.String(), "$12.99")
	assert.Contains(t, page.String(), "Old pairing.")
	items := []ai.ShoppingItem{{ProductID: "wine", Name: recommendation.Wines[0].Name, Needed: recommendation.Wines[0].Quantity}}
	prepared := prepareShoppingItems(items, nil)
	assert.Empty(t, prepared[0].VariantID, "legacy recommendations do not contain ordering metadata")
	selected := []ai.InputIngredient{{ProductID: "wine", VariantID: "selected-upc", Description: "Catalog red", Size: "750mL", PriceRegular: new(float32(15)), PriceSale: new(float32(13))}}
	key := wineStylesCacheKey([]string{"Red"}, "70500874", time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	require.NoError(t, rio.SaveIngredients(t.Context(), key, selected))
	catalog, err := rio.IngredientsFromCache(t.Context(), key)
	require.NoError(t, err)
	assert.Equal(t, selected, catalog)
	prepared = prepareShoppingItems(items, inputIngredientMap(catalog))
	assert.Equal(t, "selected-upc", prepared[0].VariantID)
	assert.Equal(t, "Catalog red", prepared[0].Name)
	assert.Equal(t, "750mL", prepared[0].Size)
	assert.Equal(t, "1 bottle", prepared[0].Needed)
	assert.Zero(t, prepared[0].PackageQuantity(), "catalog metadata must not invent a purchase quantity")
	reader, err := store.Get(t.Context(), recipeWineCacheKey(hash))
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, original, string(body), "writing a catalog snapshot does not change the saved pick")
	require.NoError(t, store.Put(t.Context(), ingredientsCachePrefix+key, "{", cache.Unconditional()))
	_, err = rio.IngredientsFromCache(t.Context(), key)
	require.Error(t, err)
}

type failingWineCatalogCache struct{ ingredientio }

func (failingWineCatalogCache) SaveIngredients(context.Context, string, []ai.InputIngredient) error {
	return errors.New("catalog write failed")
}

func TestPickAWineRequiresCachedCandidateCatalog(t *testing.T) {
	aiStub := &captureWineQuestionAIClient{selection: &ai.WineSelection{Wines: []ai.Ingredient{{ProductID: "wine", Name: "Red"}}}}
	staples := &cachedStaplesService{cache: failingWineCatalogCache{IO(cache.NewInMemoryCache())}, provider: &captureWineStaplesProvider{responses: map[string][]ai.InputIngredient{"Red": {{ProductID: "wine", VariantID: "upc", Description: "Red"}}}}}
	g := newTestGenerator(t, aiStub, nil, staples, nil, noopRecipeSaver{})
	got, err := g.PickAWine(t.Context(), "70500874", ai.Recipe{Title: "Dinner", WineStyles: []string{"Red"}}, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	require.ErrorContains(t, err, "catalog write failed")
	assert.Nil(t, got)
}

func TestWineRecommendationReadsOriginalCachePath(t *testing.T) {
	store := cache.NewInMemoryCache()
	require.NoError(t, store.Put(t.Context(), "wine_recommendations/existing-recipe", `{"wines":[{"id":"wine","name":"Existing red","quantity":"1 bottle","price":"$12.99"}],"commentary":"Original pairing."}`, cache.Unconditional()))
	selection, err := IO(store).wineFromCache(t.Context(), "existing-recipe")
	require.NoError(t, err)
	require.Len(t, selection.Wines, 1)
	assert.Equal(t, "Existing red", selection.Wines[0].Name)
	assert.Equal(t, "Original pairing.", selection.Commentary)
}

// The catalog and targeted searches run concurrently, so record calls under a lock.
type catalogWineBackend struct {
	mu           sync.Mutex
	catalogCalls int
	styleCalls   int
	catalogErr   error
	styleErr     error
}

func (*catalogWineBackend) IsID(id string) bool { return id == "70500874" || id == "70100023" }
func (*catalogWineBackend) FetchStaples(context.Context, string) ([]ai.InputIngredient, error) {
	return nil, errors.New("unexpected staples fetch")
}

func (b *catalogWineBackend) FetchWineCatalog(context.Context, string) ([]ai.InputIngredient, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.catalogCalls++
	return []ai.InputIngredient{{ProductID: "shared", VariantID: "broad-upc"}, {ProductID: "fallback", VariantID: "fallback-upc"}}, b.catalogErr
}

func (b *catalogWineBackend) FetchWines(_ context.Context, _ string, styles []string) ([]ai.InputIngredient, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.styleCalls++
	if strings.EqualFold(styles[0], "missing") {
		return nil, b.styleErr
	}
	return []ai.InputIngredient{{ProductID: "shared", VariantID: "style-upc"}, {ProductID: strings.ToLower(styles[0]), VariantID: "targeted-upc"}}, b.styleErr
}

func TestWineCatalogReusedAcrossStylesAndCombinedSnapshotSaved(t *testing.T) {
	backend := &catalogWineBackend{}
	store := cache.NewInMemoryCache()
	s := NewCachedStaplesService([]StaplesBackend{backend}, store, &stubIngredientGrader{})
	date := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, style := range []string{"Pinot Noir", "Albariño", " pinot noir "} {
		got, err := s.FetchWines(t.Context(), "70500874", []string{style}, date)
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, "style-upc", got[0].VariantID, "targeted results win duplicate IDs")
		assert.Equal(t, "fallback", got[2].ProductID)
		persisted, err := IO(store).IngredientsFromCache(t.Context(), wineStylesCacheKey([]string{style}, "70500874", date))
		require.NoError(t, err)
		assert.Equal(t, got, persisted, "shopping finalization can enrich targeted and fallback selections")
	}
	assert.Equal(t, 1, backend.catalogCalls)
	assert.Equal(t, 2, backend.styleCalls)
	for _, tc := range []struct {
		location string
		date     time.Time
	}{
		{"70100023", date}, {"70500874", date.AddDate(0, 0, 1)},
	} {
		_, err := s.FetchWines(t.Context(), tc.location, []string{"Pinot Noir"}, tc.date)
		require.NoError(t, err)
	}
	assert.Equal(t, 3, backend.catalogCalls, "broad catalog is isolated by store and date")
}

func TestWineCatalogSuppliesFallbackWhenStyleHasNoMatches(t *testing.T) {
	backend := &catalogWineBackend{}
	s := NewCachedStaplesService([]StaplesBackend{backend}, cache.NewInMemoryCache(), &stubIngredientGrader{})
	got, err := s.FetchWines(t.Context(), "70500874", []string{"Missing"}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, []ai.InputIngredient{{ProductID: "shared", VariantID: "broad-upc"}, {ProductID: "fallback", VariantID: "fallback-upc"}}, got)
}

func TestWineCatalogRequiresBothSources(t *testing.T) {
	for _, source := range []string{"catalog", "style"} {
		t.Run(source, func(t *testing.T) {
			backend := &catalogWineBackend{}
			failure := errors.New("provider failed")
			if source == "catalog" {
				backend.catalogErr = failure
			} else {
				backend.styleErr = failure
			}
			store := cache.NewInMemoryCache()
			s := NewCachedStaplesService([]StaplesBackend{backend}, store, &stubIngredientGrader{})
			date := time.Now()
			got, err := s.FetchWines(t.Context(), "70500874", []string{"Pinot Noir"}, date)
			require.ErrorIs(t, err, failure)
			assert.Nil(t, got)
			_, err = IO(store).IngredientsFromCache(t.Context(), wineStylesCacheKey([]string{"Pinot Noir"}, "70500874", date))
			assert.ErrorIs(t, err, cache.ErrNotFound)
		})
	}
}

type failingCombinedWineCache struct{ ingredientio }

func (c failingCombinedWineCache) SaveIngredients(ctx context.Context, key string, wines []ai.InputIngredient) error {
	if !strings.Contains(strings.TrimPrefix(key, "wines/"), "/") {
		return errors.New("combined catalog write failed")
	}
	return c.ingredientio.SaveIngredients(ctx, key, wines)
}

func TestWineCatalogRequiresCombinedSnapshotWrite(t *testing.T) {
	s := NewCachedStaplesService([]StaplesBackend{&catalogWineBackend{}}, cache.NewInMemoryCache(), &stubIngredientGrader{})
	s.cache = failingCombinedWineCache{s.cache}
	got, err := s.FetchWines(t.Context(), "70500874", []string{"Pinot Noir"}, time.Now())
	require.ErrorContains(t, err, "combined catalog write failed")
	assert.Nil(t, got)
}
