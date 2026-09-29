package mnfoodclub

import (
	"context"
	"errors"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubIngredientClient struct {
	ingredients []ai.InputIngredient
	err         error
	calls       int
	ctx         context.Context
}

func (c *stubIngredientClient) FetchIngredients(ctx context.Context) ([]ai.InputIngredient, error) {
	c.calls++
	c.ctx = ctx
	return c.ingredients, c.err
}

func TestMNFoodClubIdentity(t *testing.T) {
	identity := NewIdentityProvider()
	for _, id := range []string{"mnfoodclub_", "mnfoodclub_1", "mnfoodclub_delivery", "mnfoodclub_any_store"} {
		assert.True(t, identity.IsID(id), id)
	}
	for _, id := range []string{"", "mnfoodclub", "other_mnfoodclub_1", "wholefoods_1"} {
		assert.False(t, identity.IsID(id), id)
	}
	assert.Equal(t, "mnfoodclub-staples-pages-1-3-v1", identity.Signature())
}

func TestMNFoodClubFetchStaples(t *testing.T) {
	want := []ai.InputIngredient{{ProductID: "194", Description: "Avocados - qty 3"}}
	client := &stubIngredientClient{ingredients: want}
	provider := NewStaplesProvider(client)
	for _, id := range []string{"mnfoodclub_", "mnfoodclub_1", "mnfoodclub_delivery"} {
		got, err := provider.FetchStaples(t.Context(), id)
		require.NoError(t, err)
		assert.Equal(t, want, got)
		assert.Equal(t, t.Context(), client.ctx)
	}
	assert.Equal(t, 3, client.calls)
	got, err := provider.FetchStaples(t.Context(), "wholefoods_1")
	require.ErrorContains(t, err, "invalid MNFoodClub location ID")
	assert.Nil(t, got)
	assert.Equal(t, 3, client.calls)
}

func TestMNFoodClubFetchStaplesFailure(t *testing.T) {
	wantErr := errors.New("fetch failed")
	provider := NewStaplesProvider(&stubIngredientClient{err: wantErr})
	got, err := provider.FetchStaples(t.Context(), "mnfoodclub_1")
	require.ErrorIs(t, err, wantErr)
	assert.Nil(t, got)
}

func TestMNFoodClubFetchWines(t *testing.T) {
	client := &stubIngredientClient{}
	provider := NewStaplesProvider(client)
	got, err := provider.FetchWines(t.Context(), "mnfoodclub_delivery", []string{"Pinot Noir"})
	require.NoError(t, err)
	assert.Empty(t, got)
	got, err = provider.FetchWines(t.Context(), "other_1", nil)
	require.ErrorContains(t, err, "invalid MNFoodClub location ID")
	assert.Nil(t, got)
	assert.Zero(t, client.calls)
}
