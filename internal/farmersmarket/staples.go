package farmersmarket

import (
	"context"
	"fmt"

	"careme/internal/ai"
	"careme/internal/cache"
)

type staplesProvider struct {
	identityProvider
	store *store
}

func NewStaplesProvider() (*staplesProvider, error) {
	cacheStore, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create farmers market cache: %w", err)
	}
	return NewStaplesProviderFromStore(NewStore(cacheStore)), nil
}

func NewStaplesProviderFromStore(store *store) *staplesProvider {
	if store == nil {
		panic("nil store given to staples provider")
	}
	return &staplesProvider{store: store}
}

func (p *staplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	ingredients, err := p.store.freshInventory(ctx, locationID)
	if err != nil {
		return nil, err
	}
	return ingredients, nil
}

func (p *staplesProvider) FetchWines(context.Context, string, []string) ([]ai.InputIngredient, error) {
	return nil, nil
}

func (s *store) hasFreshInventory(locationID string) bool {
	_, err := s.freshInventory(context.Background(), locationID)
	return err == nil
}
