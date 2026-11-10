package mnfoodclub

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/ingredients/cachekey"
)

// weekStartDay anchors weekly cache keys and campaign generation.
const weekStartDay = time.Monday

const catalogVersion = "mnfoodclub-bigcommerce-v1"

// The existing storefront's Wine & Wine Alternatives category.
const wineCategoryID = 178

type identityProvider struct{}

func (p identityProvider) HashForStore(locationID string, date time.Time) hash.Hash {
	date = cachekey.StoreDay(date)
	date = date.AddDate(0, 0, -int((date.Weekday()-weekStartDay+7)%7))
	signatureParts := []string{catalogVersion}
	for _, ingredient := range produceShareIngredients() {
		signatureParts = append(signatureParts, ingredient.Description)
	}
	signature := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(signatureParts, "\n"))))
	// We could generate recipes once and copy them to all three storefront hashes,
	// but each hash needs its own params object to preserve the store name and ID.
	return cachekey.NewHash(locationID, date, signature)
}

func NewIdentityProvider() identityProvider { return identityProvider{} }

func (identityProvider) IsID(locationID string) bool {
	return locationID == MNFoodClubLocationID ||
		locationID == TCFarmLocationID ||
		locationID == NelsonGrassFarmLocationID
}

type ingredientClient interface {
	Fetch(context.Context, int) ([]ai.InputIngredient, error)
}

// staplesProvider serves the API catalog and static produce shares for every
// MN Food Club, TC Farm, and Nelson Grass Farm delivery location.
type staplesProvider struct {
	identityProvider
	client ingredientClient
}

func NewStaplesProvider(client ingredientClient) staplesProvider {
	return staplesProvider{client: client}
}

func (p staplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid MNFoodClub location ID %q", locationID)
	}
	ingredients, err := p.client.Fetch(ctx, allCategories)
	if err != nil {
		return nil, fmt.Errorf("fetch MNFoodClub staples: %w", err)
	}
	return append(ingredients, produceShareIngredients()...), nil
}

// FetchWines fetches every page of wines and wine alternatives.
func (p staplesProvider) FetchWines(ctx context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid MNFoodClub location ID %q", locationID)
	}
	wines, err := p.client.Fetch(ctx, wineCategoryID)
	if err != nil {
		return nil, fmt.Errorf("fetch MNFoodClub wines: %w", err)
	}
	return wines, nil
}
