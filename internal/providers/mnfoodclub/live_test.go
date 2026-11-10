package mnfoodclub

import (
	"net/http"
	"os"
	"testing"
	"time"

	"careme/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Opt-in only: reads the real catalog without calling the grader or saving products.
func TestLiveBigCommerceCatalog(t *testing.T) {
	if os.Getenv("MNFOODCLUB_LIVE_TEST") != "1" {
		t.Skip("set MNFOODCLUB_LIVE_TEST=1 and MNFOODCLUB_* credentials for read-only API verification")
	}
	client, err := NewClient(config.MNFoodClubConfig{
		APIPath: os.Getenv("MNFOODCLUB_API_PATH"), AccessToken: os.Getenv("MNFOODCLUB_ACCESS_TOKEN"),
	}, &http.Client{Timeout: 30 * time.Second})
	require.NoError(t, err)
	provider := NewStaplesProvider(client)
	staples, err := provider.FetchStaples(t.Context(), "mnfoodclub_delivery")
	require.NoError(t, err)
	require.Greater(t, len(staples), len(produceShareIngredients()))
	catalogCount := len(staples) - len(produceShareIngredients())
	for _, ingredient := range staples[:catalogCount] {
		assert.NotEmpty(t, ingredient.ProductID)
		assert.NotEmpty(t, ingredient.Description)
		assert.NotNil(t, ingredient.PriceRegular)
		assert.Nil(t, ingredient.Grade)
	}
	wines, err := provider.FetchWines(t.Context(), "mnfoodclub_delivery", nil)
	require.NoError(t, err)
	require.NotEmpty(t, wines)
	t.Logf("Imported %d catalog products + %d produce-share ingredients; %d wine candidates", catalogCount, len(produceShareIngredients()), len(wines))
	for _, ingredient := range staples[:min(3, catalogCount)] {
		t.Logf("Sample: id=%s name=%q size=%q brand=%q aisle=%q price=%.2f", ingredient.ProductID, ingredient.Description, ingredient.Size, ingredient.Brand, ingredient.AisleNumber, *ingredient.PriceRegular)
	}
}
