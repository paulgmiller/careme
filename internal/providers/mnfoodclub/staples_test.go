package mnfoodclub

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/ingredients/cachekey"

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
	for _, id := range []string{"mnfoodclub_delivery", "tcfarm_delivery", "nelsongrassfarm_delivery"} {
		assert.True(t, identity.IsID(id), id)
	}
	for _, id := range []string{"", "mnfoodclub", "other_mnfoodclub_1", "wholefoods_1", "tcfarm_other", "nelsongrassfarm_other"} {
		assert.False(t, identity.IsID(id), id)
	}
	parts := []string{"mnfoodclub-bigcommerce-v1"}
	for _, ingredient := range produceShareIngredients() {
		parts = append(parts, ingredient.Description)
	}
	signature := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\n"))))
	monday := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	want := cachekey.NewHash("mnfoodclub_delivery", monday, signature).Sum(nil)
	assert.Equal(t, want, identity.HashForStore("mnfoodclub_delivery", monday.AddDate(0, 0, 2).Add(12*time.Hour)).Sum(nil))
}

func TestMNFoodClubHashAnchorsEachDayToMonday(t *testing.T) {
	identity := NewIdentityProvider()
	zone, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	for _, monday := range []time.Time{
		time.Date(2026, 10, 19, 0, 0, 0, 0, zone),
		time.Date(2025, 12, 29, 0, 0, 0, 0, zone),
		time.Date(2026, 3, 2, 0, 0, 0, 0, zone),
		time.Date(2026, 10, 26, 0, 0, 0, 0, zone),
	} {
		for day := range 7 {
			date := monday.AddDate(0, 0, day).Add(12 * time.Hour)
			t.Run(date.Format(time.DateOnly)+"_"+date.Weekday().String(), func(t *testing.T) {
				// Build the expected hash from a known Monday, independently of
				// the provider's weekday arithmetic.
				parts := []string{catalogVersion}
				for _, ingredient := range produceShareIngredients() {
					parts = append(parts, ingredient.Description)
				}
				signature := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\n"))))
				want := cachekey.NewHash("mnfoodclub_delivery", monday, signature).Sum(nil)
				assert.Equal(t, want, identity.HashForStore("mnfoodclub_delivery", date).Sum(nil))
			})
		}
	}
}

func TestMNFoodClubFetchStaples(t *testing.T) {
	want := []ai.InputIngredient{{ProductID: "194", Description: "Avocados - qty 3"}, {ProductID: "195", Description: "Ground beef"}}
	client := &stubIngredientClient{ingredients: want}
	provider := NewStaplesProvider(client)
	for _, id := range []string{"mnfoodclub_delivery", "tcfarm_delivery", "nelsongrassfarm_delivery"} {
		client.calls = nil
		got, err := provider.FetchStaples(t.Context(), id)
		require.NoError(t, err)
		require.Len(t, got, len(want)+len(produceShareIngredients()))
		assert.Equal(t, want, got[:len(want)])
		assert.Equal(t, produceShareIngredients(), got[len(want):])
		assert.Equal(t, t.Context(), client.ctx)
		assert.Equal(t, []int{allCategories}, client.calls)
	}
}

func TestMNFoodClubFetchWines(t *testing.T) {
	want := []ai.InputIngredient{{ProductID: "197", Description: "Pinot Noir"}}
	client := &stubIngredientClient{ingredients: want}
	for _, id := range []string{"mnfoodclub_delivery", "tcfarm_delivery", "nelsongrassfarm_delivery"} {
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
