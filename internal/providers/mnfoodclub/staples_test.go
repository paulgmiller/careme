package mnfoodclub

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubIngredientClient struct {
	ingredients []ai.InputIngredient
	err         error
	calls       []int
	ctx         context.Context
}

func (c *stubIngredientClient) Fetch(ctx context.Context, categoryID int) ([]ai.InputIngredient, error) {
	c.calls = append(c.calls, categoryID)
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
	parts := []string{"mnfoodclub-bigcommerce-v1"}
	for _, ingredient := range produceShareIngredients() {
		parts = append(parts, ingredient.Description)
	}
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\n")))), identity.Signature())
}

func TestMNFoodClubFetchStaples(t *testing.T) {
	want := []ai.InputIngredient{{ProductID: "194", Description: "Avocados - qty 3"}, {ProductID: "195", Description: "Ground beef"}}
	client := &stubIngredientClient{ingredients: want}
	provider := NewStaplesProvider(client)
	for _, id := range []string{"mnfoodclub_", "mnfoodclub_1", "mnfoodclub_delivery", "mnfoodclub_tcfarm_delivery", "mnfoodclub_nelsongrassfarm_delivery"} {
		client.calls = nil
		got, err := provider.FetchStaples(t.Context(), id)
		require.NoError(t, err)
		require.Len(t, got, len(want)+len(produceShareIngredients()))
		assert.Equal(t, want, got[:len(want)])
		assert.Equal(t, produceShareIngredients(), got[len(want):])
		assert.Equal(t, t.Context(), client.ctx)
		assert.Equal(t, []int{AllCategories}, client.calls)
	}
}

func TestMNFoodClubFetchWines(t *testing.T) {
	want := []ai.InputIngredient{{ProductID: "197", Description: "Pinot Noir"}}
	client := &stubIngredientClient{ingredients: want}
	for _, id := range []string{"mnfoodclub_delivery", "mnfoodclub_tcfarm_delivery", "mnfoodclub_nelsongrassfarm_delivery"} {
		client.calls = nil
		got, err := NewStaplesProvider(client).FetchWines(t.Context(), id, []string{"Pinot Noir"})
		require.NoError(t, err)
		assert.Equal(t, want, got)
		assert.Equal(t, t.Context(), client.ctx)
		assert.Equal(t, []int{178}, client.calls)
	}
}

func TestMNFoodClubInvalidLocationsAndFailures(t *testing.T) {
	for _, wine := range []bool{false, true} {
		t.Run(fmt.Sprintf("wine=%t", wine), func(t *testing.T) {
			wantErr := errors.New("fetch failed")
			client := &stubIngredientClient{ingredients: []ai.InputIngredient{{ProductID: "1"}}, err: wantErr}
			provider := NewStaplesProvider(client)
			fetch := func(id string) ([]ai.InputIngredient, error) {
				if wine {
					return provider.FetchWines(t.Context(), id, nil)
				}
				return provider.FetchStaples(t.Context(), id)
			}
			got, err := fetch("other_1")
			require.ErrorContains(t, err, "invalid MNFoodClub location ID")
			assert.Nil(t, got)
			assert.Empty(t, client.calls)
			got, err = fetch("mnfoodclub_delivery")
			require.ErrorIs(t, err, wantErr)
			assert.Nil(t, got)
			assert.Len(t, client.calls, 1)
		})
	}
}
