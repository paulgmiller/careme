package recipes

import (
	"bytes"
	"context"
	"errors"
	"io"
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
