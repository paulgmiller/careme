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

func TestWineCatalogSidecarPreservesRecommendationAndEnrichesShoppingItem(t *testing.T) {
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
	food := []shoppingPurchaseGroup{{Aisle: "Produce", Items: []shoppingPurchaseItem{{ProductID: "food", Name: "Food"}}}}
	picks := map[string]*ai.WineSelection{hash: recommendation}
	catalog, err := rio.wineCatalogForRecipes(t.Context(), []ai.Recipe{recipe})
	require.NoError(t, err)
	purchases, err := prepareShoppingPurchases(food, []ai.Recipe{recipe}, picks, catalog)
	require.NoError(t, err)
	assert.Empty(t, purchases[1].Items[0].VariantID, "legacy recommendation identifiers are not used for ordering")
	selected := []ai.InputIngredient{{ProductID: "wine", VariantID: "selected-upc", Description: "Catalog red", Size: "750mL", PriceRegular: new(float32(15)), PriceSale: new(float32(13))}}
	require.NoError(t, rio.saveWineIngredients(t.Context(), hash, selected))
	catalog, err = rio.wineCatalogForRecipes(t.Context(), []ai.Recipe{recipe})
	require.NoError(t, err)
	assert.Equal(t, selected, catalog)
	purchases, err = prepareShoppingPurchases(food, []ai.Recipe{recipe}, picks, catalog)
	require.NoError(t, err)
	assert.Equal(t, "selected-upc", purchases[1].Items[0].VariantID)
	assert.Equal(t, "Catalog red", purchases[1].Items[0].Name)
	assert.Equal(t, "750mL", purchases[1].Items[0].Size)
	assert.Equal(t, "1 bottle", purchases[1].Items[0].Needed)
	assert.Equal(t, 1, purchases[1].Items[0].PackageQuantity())
	reader, err := store.Get(t.Context(), recipeWineCacheKey(hash))
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, original, string(body), "writing a catalog snapshot does not change the saved pick")
	require.NoError(t, store.Put(t.Context(), selectedWineIngredientsPrefix+hash, "{", cache.Unconditional()))
	_, err = rio.wineCatalogForRecipes(t.Context(), []ai.Recipe{recipe})
	require.ErrorContains(t, err, "decode selected wine ingredients")
}

type failingWineCatalogSaver struct{ noopRecipeSaver }

func (failingWineCatalogSaver) saveWineIngredients(context.Context, string, []ai.InputIngredient) error {
	return errors.New("catalog write failed")
}

func TestPickAWineRequiresCatalogSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, id, want string
		saver          recipeSaver
	}{
		{"unknown product", "missing", "not in the supplied catalog", noopRecipeSaver{}},
		{"catalog save failure", "wine", "catalog write failed", failingWineCatalogSaver{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			aiStub := &captureWineQuestionAIClient{selection: &ai.WineSelection{Wines: []ai.Ingredient{{ProductID: tc.id, Name: "Red"}}}}
			staples := &cachedStaplesService{cache: IO(cache.NewInMemoryCache()), provider: &captureWineStaplesProvider{responses: map[string][]ai.InputIngredient{"Red": {{ProductID: "wine", VariantID: "upc", Description: "Red"}}}}}
			g := newTestGenerator(t, aiStub, nil, staples, nil, tc.saver)
			got, err := g.PickAWine(t.Context(), "70500874", ai.Recipe{Title: "Dinner", WineStyles: []string{"Red"}}, time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, got)
		})
	}
}
