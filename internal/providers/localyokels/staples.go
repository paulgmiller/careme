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
	baseURL        = "https://localyokels.localfoodmarketplace.com"
	locationID     = "localyokels_market"
	wineCategoryID = 1220 // Beer, Wine, and Cider.
)

// Former selection IDs, kept for possible category filtering later.
// const (
// 	meatsCategoryID = 1230
// 	vegetablesCategoryID = 1232
// 	dairyAndEggsCategoryID = 1222
// 	fruitCategoryID = 1225
// 	pastaSubcategoryID = 1289
// )

type identityProvider struct{}

func NewIdentityProvider() identityProvider  { return identityProvider{} }
func (identityProvider) IsID(id string) bool { return id == locationID }
func (identityProvider) HashForStore(id string, date time.Time) hash.Hash {
	return cachekey.NewHash(id, cachekey.StoreDay(date), baseURL+"/all-products/v3")
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
	// Keep the full catalog for the ingredient grader and shared ingredient cache.
	items, err := p.client.Fetch(ctx, localfoodmarketplace.Selection{})
	if err != nil {
		return nil, fmt.Errorf("fetch Local Yokels staples: %w", err)
	}
	return items, nil
}

func (p staplesProvider) FetchWines(ctx context.Context, id string, _ []string) ([]ai.InputIngredient, error) {
	if !p.IsID(id) {
		return nil, fmt.Errorf("invalid Local Yokels location ID %q", id)
	}
	// Return all beer, wine, and cider candidates; pairing chooses among them using recipe styles.
	items, err := p.client.Fetch(ctx, localfoodmarketplace.Selection{CategoryIDs: []int{wineCategoryID}})
	if err != nil {
		return nil, fmt.Errorf("fetch Local Yokels wines: %w", err)
	}
	return items, nil
}
