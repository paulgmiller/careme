package kroger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/kroger/products"
	"careme/internal/seasons"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCocktailCatalog(t *testing.T) {
	var terms []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "70500874", r.URL.Query().Get("filter.locationId"))
		assert.Equal(t, "12", r.URL.Query().Get("filter.limit"))
		assert.Equal(t, "ais", r.URL.Query().Get("filter.fulfillment"))
		terms = append(terms, r.URL.Query().Get("filter.term"))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"data":[{"productId":"gin","description":"Gin","items":[{"price":{"regular":20}}]},{"productId":"out","description":"Unavailable","items":[{"price":{"regular":2},"inventory":{"stockLevel":"TEMPORARILY_OUT_OF_STOCK"}}]},{"productId":"incomplete"}]}`))
		assert.NoError(t, err)
	}))
	defer srv.Close()
	client, err := products.NewClientWithResponses(srv.URL)
	require.NoError(t, err)
	provider := CocktailProvider{client: client}
	ingredients, err := provider.FetchCocktailIngredients(context.Background(), "70500874", seasons.Fall)
	require.NoError(t, err)
	require.Len(t, ingredients, 1)
	assert.Equal(t, "gin", ingredients[0].ProductID)
	assert.Contains(t, terms, "apple cider")
	assert.Contains(t, terms, "fresh mint")
	assert.NotContains(t, terms, "beef")
	assert.NotContains(t, terms, "fresh produce")
}

func TestCocktailSearchSeasons(t *testing.T) {
	for season, term := range map[seasons.Season]string{seasons.Spring: "strawberries", seasons.Summer: "watermelon", seasons.Fall: "pears", seasons.Winter: "grapefruit"} {
		assert.Contains(t, cocktailTerms(season), term)
	}
}

func TestCocktailCatalogFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	client, err := products.NewClientWithResponses(srv.URL)
	require.NoError(t, err)
	_, err = (&CocktailProvider{client: client}).FetchCocktailIngredients(t.Context(), "70500874", seasons.Fall)
	require.ErrorContains(t, err, "search gin")
}
