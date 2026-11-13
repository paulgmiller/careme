package fullcircle

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"careme/internal/ingredients/cachekey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProduceBoxIngredients(t *testing.T) {
	items := produceBoxIngredients()
	require.Len(t, items, 18)
	want := []struct{ name, size string }{
		{"Crimini Mushroom", "0.5 lb"},
		{"Maitake Mushroom", "1 package (4 oz)"},
		{"Chef's Sampler Mushroom", "1 package (8 oz)"},
		{"King Richard Leek", "1 bunch"},
		{"Dill, Small", "1 package"},
		{"Rainbow Chard", "1 bunch"},
		{"Gold Topped Beets", "1 lb"},
		{"Baby Broccoli", "1 bunch"},
		{"Spaghetti Winter Squash", "1"},
		{"Ambrosia Apple", "2"},
		{"Celery", "1"},
		{"Flat Leaf Parsley", "1 bunch"},
		{"Green Kale", "1 bunch"},
		{"Kiwi", "2"},
		{"Red Grapefruit", "1"},
		{"Red Onion", "1"},
		{"Red Potatoes", "1 lb"},
		{"Red Topped Beets", "1 lb"},
	}
	ids := map[string]bool{}
	for i, item := range items {
		assert.Equal(t, "Produce box "+want[i].name, item.Description)
		assert.Equal(t, want[i].size, item.Size)
		assert.True(t, strings.HasPrefix(item.ProductID, "fullcircle_produce_box_"))
		assert.False(t, ids[item.ProductID], "duplicate product ID")
		ids[item.ProductID] = true
		assert.Equal(t, "Full Circle", item.Brand)
		assert.Equal(t, []string{"Produce", "Produce box"}, item.Categories)
		assert.Nil(t, item.PriceRegular)
		assert.Nil(t, item.PriceSale)
		require.NotNil(t, item.Grade)
		assert.Equal(t, 10, item.Grade.Score)
		assert.NotEmpty(t, item.Grade.Reason)
	}
	items[0].Grade.Score = 0
	items[0].Categories[0] = "Changed"
	fresh := produceBoxIngredients()
	assert.Equal(t, 10, fresh[0].Grade.Score)
	assert.Equal(t, "Produce", fresh[0].Categories[0])
}

func TestProduceBoxInvalidatesCatalogOnlyCache(t *testing.T) {
	date := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	parts := []string{"fullcircle-public-catalog-v1"}
	for _, category := range stapleCategories {
		parts = append(parts, baseURL+category.path, category.name)
	}
	oldSignature := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\n"))))
	oldHash := cachekey.NewHash(locationID, date, oldSignature)
	assert.NotEqual(t, oldHash.Sum(nil), NewIdentityProvider().HashForStore(locationID, date).Sum(nil))
}
