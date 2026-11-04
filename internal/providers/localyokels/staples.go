// Package localyokels sources staples from Local Yokels Market.
package localyokels

import (
	"context"
	"fmt"
	"hash"
	"time"

	"careme/internal/ai"
	"careme/internal/ingredients/cachekey"
	"careme/internal/localfoodmarketplace"
)

const (
	baseURL    = "https://localyokels.localfoodmarketplace.com"
	locationID = "localyokels_market"
)

type identityProvider struct{}

func NewIdentityProvider() identityProvider  { return identityProvider{} }
func (identityProvider) IsID(id string) bool { return id == locationID }
func (identityProvider) HashForStore(id string, date time.Time) hash.Hash {
	return cachekey.NewHash(id, cachekey.StoreDay(date), baseURL+"/categories/1230/1232/1222/1225/subcategories/1289/v2")
}

type catalogClient interface {
	Fetch(context.Context, localfoodmarketplace.Selection) ([]ai.InputIngredient, error)
}

type staplesProvider struct {
	identityProvider
	client catalogClient
}

func NewStaplesProvider(client catalogClient) staplesProvider { return staplesProvider{client: client} }
func (p staplesProvider) FetchStaples(ctx context.Context, id string) ([]ai.InputIngredient, error) {
	if !p.IsID(id) {
		return nil, fmt.Errorf("invalid Local Yokels location ID %q", id)
	}
	items, err := p.client.Fetch(ctx, localfoodmarketplace.Selection{
		CategoryIDs:    []int{1230, 1232, 1222, 1225},
		SubcategoryIDs: []int{1289},
	})
	if err != nil {
		return nil, fmt.Errorf("fetch Local Yokels staples: %w", err)
	}
	return items, nil
}

func (p staplesProvider) FetchWines(_ context.Context, id string, _ []string) ([]ai.InputIngredient, error) {
	return nil, fmt.Errorf("wine lookup is not supported for location %q", id)
}
