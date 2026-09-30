package mnfoodclub

import (
	"context"
	"errors"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fetchCall struct {
	url   string
	pages int
}

type stubIngredientClient struct {
	ingredients map[string][]ai.InputIngredient
	err         error
	failURL     string
	calls       []fetchCall
	ctx         context.Context
}

func (c *stubIngredientClient) Fetch(ctx context.Context, url string, pages int) ([]ai.InputIngredient, error) {
	c.calls = append(c.calls, fetchCall{url, pages})
	c.ctx = ctx
	if url == c.failURL {
		return nil, c.err
	}
	return c.ingredients[url], nil
}

func TestMNFoodClubIdentity(t *testing.T) {
	identity := NewIdentityProvider()
	for _, id := range []string{"mnfoodclub_", "mnfoodclub_1", "mnfoodclub_delivery", "mnfoodclub_any_store"} {
		assert.True(t, identity.IsID(id), id)
	}
	for _, id := range []string{"", "mnfoodclub", "other_mnfoodclub_1", "wholefoods_1"} {
		assert.False(t, identity.IsID(id), id)
	}
	assert.Equal(t, 64, len(identity.Signature()))
}

func TestMNFoodClubSignatureTracksStapleURLs(t *testing.T) {
	identity := NewIdentityProvider()
	original := identity.Signature()
	assert.Equal(t, original, identity.Signature())

	originalURL := stapleCategories[0].url
	t.Cleanup(func() { stapleCategories[0].url = originalURL })
	stapleCategories[0].url = "https://mnfood.club/shop-all/produce/"
	assert.NotEqual(t, original, identity.Signature())
}

func TestMNFoodClubFetchStaples(t *testing.T) {
	produce := ai.InputIngredient{ProductID: "194", Description: "Avocados - qty 3"}
	meat := ai.InputIngredient{ProductID: "195", Description: "Ground beef"}
	pasta := ai.InputIngredient{ProductID: "196", Description: "Spaghetti"}
	seafood := ai.InputIngredient{ProductID: "197", Description: "Salmon"}
	client := &stubIngredientClient{ingredients: map[string][]ai.InputIngredient{
		"https://mnfood.club/shop-all/produce/?sort=bestselling": {produce},
		"https://mnfood.club/shop-all/meat/":                     {meat},
		"https://mnfood.club/shop/pantry/pasta/":                 {pasta},
		"https://mnfood.club/shop-all/fish-seafood/":             {seafood},
	}}
	provider := NewStaplesProvider(client)
	for _, id := range []string{"mnfoodclub_", "mnfoodclub_1", "mnfoodclub_delivery"} {
		client.calls = nil
		got, err := provider.FetchStaples(t.Context(), id)
		require.NoError(t, err)
		require.Len(t, got, 40)
		assert.Equal(t, []ai.InputIngredient{produce, meat, pasta, seafood}, got[:4])
		assert.Equal(t, produceShareIngredients(), got[4:])
		assert.Equal(t, t.Context(), client.ctx)
		assert.Equal(t, []fetchCall{
			{"https://mnfood.club/shop-all/produce/?sort=bestselling", 3},
			{"https://mnfood.club/shop-all/meat/", 3},
			{"https://mnfood.club/shop/pantry/pasta/", 1},
			{"https://mnfood.club/shop-all/fish-seafood/", 1},
		}, client.calls)
	}
	client.calls = nil
	got, err := provider.FetchStaples(t.Context(), "wholefoods_1")
	require.ErrorContains(t, err, "invalid MNFoodClub location ID")
	assert.Nil(t, got)
	assert.Empty(t, client.calls)
}

func TestMNFoodClubFetchStaplesFailure(t *testing.T) {
	for i, url := range []string{
		"https://mnfood.club/shop-all/produce/?sort=bestselling",
		"https://mnfood.club/shop-all/meat/",
		"https://mnfood.club/shop/pantry/pasta/",
		"https://mnfood.club/shop-all/fish-seafood/",
	} {
		t.Run(url, func(t *testing.T) {
			wantErr := errors.New("fetch failed")
			client := &stubIngredientClient{
				err: wantErr, failURL: url,
				ingredients: map[string][]ai.InputIngredient{
					"https://mnfood.club/shop-all/produce/?sort=bestselling": {{ProductID: "194"}},
					"https://mnfood.club/shop-all/meat/":                     {{ProductID: "195"}},
				},
			}
			got, err := NewStaplesProvider(client).FetchStaples(t.Context(), "mnfoodclub_1")
			require.ErrorIs(t, err, wantErr)
			assert.Contains(t, err.Error(), url)
			assert.Nil(t, got)
			assert.Len(t, client.calls, i+1)
		})
	}
}

func TestMNFoodClubFetchWines(t *testing.T) {
	url := "https://mnfood.club/shop/beverage/n-a-tasty-drinks/wine-wine-alternatives/"
	want := []ai.InputIngredient{{ProductID: "197", Description: "Pinot Noir"}}
	client := &stubIngredientClient{ingredients: map[string][]ai.InputIngredient{url: want}}
	provider := NewStaplesProvider(client)
	got, err := provider.FetchWines(t.Context(), "mnfoodclub_delivery", []string{"Pinot Noir"})
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, t.Context(), client.ctx)
	assert.Equal(t, []fetchCall{{url, 1}}, client.calls)
	client.calls = nil
	got, err = provider.FetchWines(t.Context(), "other_1", nil)
	require.ErrorContains(t, err, "invalid MNFoodClub location ID")
	assert.Nil(t, got)
	assert.Empty(t, client.calls)
}

func TestMNFoodClubFetchWinesFailure(t *testing.T) {
	wantErr := errors.New("fetch failed")
	client := &stubIngredientClient{
		err:     wantErr,
		failURL: "https://mnfood.club/shop/beverage/n-a-tasty-drinks/wine-wine-alternatives/",
	}
	got, err := NewStaplesProvider(client).FetchWines(t.Context(), "mnfoodclub_1", nil)
	require.ErrorIs(t, err, wantErr)
	assert.Nil(t, got)
	assert.Len(t, client.calls, 1)
}
