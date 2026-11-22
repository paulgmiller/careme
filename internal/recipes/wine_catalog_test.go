package recipes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	got, err := pickWine(t.Context(), g.aiClient, g.staples, "70500874", ai.Recipe{Title: "Dinner", WineStyles: []string{"Red"}}, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
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

// A fixed-catalog backend models providers whose queries ignore recipe styles.
type fixedWineCatalogBackend struct {
	mu    sync.Mutex
	calls int
}

func (*fixedWineCatalogBackend) IsID(id string) bool {
	return id == "wholefoods_1" || id == "wholefoods_2"
}
func (*fixedWineCatalogBackend) WineCacheStyles([]string) []string { return nil }
func (*fixedWineCatalogBackend) FetchStaples(context.Context, string) ([]ai.InputIngredient, error) {
	return nil, errors.New("unexpected staples fetch")
}

func (b *fixedWineCatalogBackend) FetchWines(context.Context, string, []string) ([]ai.InputIngredient, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	return []ai.InputIngredient{{ProductID: "wine", VariantID: "wine-upc", Description: "Catalog wine"}}, nil
}

func TestFixedWineCatalogReusedAcrossRecipeStyles(t *testing.T) {
	backend := &fixedWineCatalogBackend{}
	store := cache.NewInMemoryCache()
	s := NewCachedStaplesService([]StaplesBackend{backend}, store, &stubIngredientGrader{})
	date := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, style := range []string{"Pinot Noir", "Albariño", "Chardonnay"} {
		got, err := s.FetchWines(t.Context(), "wholefoods_1", []string{style}, date)
		require.NoError(t, err)
		require.Len(t, got, 1)
		persisted, err := IO(store).IngredientsFromCache(t.Context(), wineStylesCacheKey([]string{style}, "wholefoods_1", date))
		require.NoError(t, err)
		assert.Equal(t, got, persisted, "each recipe retains ordering metadata")
	}
	assert.Equal(t, 1, backend.calls)
	// The canonical cache also survives a new service instance.
	s = NewCachedStaplesService([]StaplesBackend{backend}, store, &stubIngredientGrader{})
	_, err := s.FetchWines(t.Context(), "wholefoods_1", []string{"Barbera"}, date)
	require.NoError(t, err)
	assert.Equal(t, 1, backend.calls)
	for _, tc := range []struct {
		location string
		date     time.Time
	}{{"wholefoods_2", date}, {"wholefoods_1", date.AddDate(0, 0, 1)}} {
		_, err := s.FetchWines(t.Context(), tc.location, []string{"Pinot Noir"}, tc.date)
		require.NoError(t, err)
	}
	assert.Equal(t, 3, backend.calls, "store and date remain isolated")
}

func TestFixedWineCatalogConcurrentStylesShareFetch(t *testing.T) {
	backend := &fixedWineCatalogBackend{}
	s := NewCachedStaplesService([]StaplesBackend{backend}, cache.NewInMemoryCache(), &stubIngredientGrader{})
	date := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	errors := make(chan error, 12)
	for i := range 12 {
		go func() {
			<-start
			_, err := s.FetchWines(t.Context(), "wholefoods_1", []string{fmt.Sprintf("style-%d", i)}, date)
			errors <- err
		}()
	}
	close(start)
	for range 12 {
		require.NoError(t, <-errors)
	}
	assert.Equal(t, 1, backend.calls)
}

func TestStyleSensitiveWineCatalogsRemainSeparate(t *testing.T) {
	backend := &stubStaplesProvider{ids: map[string]bool{"store": true}, ingredients: []ai.InputIngredient{{ProductID: "wine"}}}
	s := NewCachedStaplesService([]StaplesBackend{backend}, cache.NewInMemoryCache(), &stubIngredientGrader{})
	date := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, style := range []string{"Pinot Noir", "Chardonnay", "Pinot Noir"} {
		_, err := s.FetchWines(t.Context(), "store", []string{style}, date)
		require.NoError(t, err)
	}
	assert.Equal(t, 2, backend.calls)
}

type failingWineSnapshotCache struct {
	ingredientio
	key string
}

func (c failingWineSnapshotCache) SaveIngredients(ctx context.Context, key string, wines []ai.InputIngredient) error {
	if key == c.key {
		return errors.New("snapshot write failed")
	}
	return c.ingredientio.SaveIngredients(ctx, key, wines)
}

func TestFixedWineCatalogRequiresRecipeSnapshot(t *testing.T) {
	date := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	s := NewCachedStaplesService([]StaplesBackend{&fixedWineCatalogBackend{}}, cache.NewInMemoryCache(), &stubIngredientGrader{})
	s.cache = failingWineSnapshotCache{s.cache, wineStylesCacheKey([]string{"Pinot Noir"}, "wholefoods_1", date)}
	got, err := s.FetchWines(t.Context(), "wholefoods_1", []string{"Pinot Noir"}, date)
	require.ErrorContains(t, err, "snapshot write failed")
	assert.Nil(t, got)
}
