package providerregistry_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mnFoodClubGrader struct{ ingredients []ai.InputIngredient }

func (g *mnFoodClubGrader) GradeIngredients(_ context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	g.ingredients = append([]ai.InputIngredient(nil), ingredients...)
	for i := range ingredients {
		if ingredients[i].Grade == nil {
			ingredients[i].Grade = &ai.IngredientGrade{Score: 9, Reason: "test grade"}
		}
	}
	return ingredients, nil
}

func TestMNFoodClubAPIRoutingGradingAndOutput(t *testing.T) {
	var wineCalls int
	client := &http.Client{Transport: smithBrothersTransport(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "test-token", req.Header.Get("X-Auth-Token"))
		var data string
		switch req.URL.Path {
		case "/stores/test/v3/catalog/products":
			data = `[{"id":194,"name":"Avocados - qty 3","price":6.99,"brand_id":1,"categories":[2],"is_visible":true,"availability":"available","inventory_tracking":"none","variants":[{"calculated_price":6.99}]}]`
			if req.URL.Query().Get("categories:in") == "178" {
				wineCalls++
				data = `[{"id":197,"name":"Pinot Noir - 750ml","price":15,"brand_id":1,"categories":[178],"is_visible":true,"availability":"available","variants":[{"calculated_price":12}]}]`
			}
		case "/stores/test/v3/catalog/categories":
			data = `[{"id":2,"name":"Produce"},{"id":178,"name":"Wine & Wine Alternatives"}]`
		case "/stores/test/v3/catalog/brands":
			data = `[{"id":1,"name":"Local Farm"}]`
		default:
			t.Fatalf("unexpected request: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":` + data + `,"meta":{"pagination":{"current_page":1,"total_pages":1}}}`))}, nil
	})}
	backend := mnfoodclub.NewStaplesProvider(mnfoodclub.NewClient(config.MNFoodClubConfig{APIPath: "https://api.bigcommerce.com/stores/test/v3", AccessToken: "test-token"}, client))
	grader := &mnFoodClubGrader{}
	service := recipes.NewCachedStaplesService([]recipes.StaplesBackend{backend}, cache.NewInMemoryCache(), grader)
	date := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	got, err := service.FetchStaples(t.Context(), recipes.DefaultParams(&locations.Location{ID: "mnfoodclub_delivery"}, date))
	require.NoError(t, err)
	require.Len(t, got, 33)
	require.Len(t, grader.ingredients, 33)
	assert.Nil(t, grader.ingredients[0].Grade)
	assert.Equal(t, 9, got[0].Grade.Score)
	for _, share := range grader.ingredients[1:] {
		assert.Equal(t, 10, share.Grade.Score)
	}
	var rendered bytes.Buffer
	require.NoError(t, ai.InputIngredientsToTSV(got, &rendered))
	assert.Contains(t, rendered.String(), "Avocados - qty 3")
	assert.Contains(t, rendered.String(), "3 count")
	assert.Contains(t, rendered.String(), "6.99")
	assert.Contains(t, rendered.String(), "Produce share seasonal Rutabaga")
	wines, err := service.FetchWines(t.Context(), "mnfoodclub_delivery", []string{"Pinot Noir"}, date)
	require.NoError(t, err)
	require.Len(t, wines, 1)
	assert.Equal(t, "197", wines[0].ProductID)
	assert.Equal(t, float32(15), *wines[0].PriceRegular)
	assert.Equal(t, float32(12), *wines[0].PriceSale)
	assert.Equal(t, 1, wineCalls)
}
