package recipes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"careme/internal/albertsons"
	"careme/internal/brightdata"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/parallelism"
	"careme/internal/walmart"
	"careme/internal/wholefoods"

	"github.com/samber/lo/mutable"
)

// todo make this a indepenedent ingredient object not kroger.
type staplesProvider interface {
	FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error)
	GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]kroger.Ingredient, error)
}

type identityProvider interface {
	IsID(locationID string) bool
	Signature() string
}

type routingStaplesProvider struct {
	backends []backendStaplesProvider
}

type backendStaplesProvider interface {
	IsID(locationID string) bool
	Signature() string
	staplesProvider
}

type ingredientio interface {
	SaveIngredients(ctx context.Context, hash string, ingredients []kroger.Ingredient) error
	IngredientsFromCache(ctx context.Context, hash string) ([]kroger.Ingredient, error)
}

type cachedStaplesService struct {
	provider staplesProvider
	cache    ingredientio
}

func NewStaplesProvider(cfg *config.Config) (staplesProvider, error) {
	kclient, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	backends, err := defaultStaplesBackends(cfg, kclient)
	if err != nil {
		return nil, err
	}

	return routingStaplesProvider{
		backends: backends,
	}, nil
}

func NewCachedStaplesService(cfg *config.Config, c cache.Cache) (StaplesService, error) {
	provider, err := NewStaplesProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create staples provider: %w", err)
	}
	return &cachedStaplesService{
		provider: provider,
		cache:    IO(c),
	}, nil
}

func (p routingStaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error) {
	provider, err := p.providerForLocation(locationID)
	if err != nil {
		return nil, err
	}
	return provider.FetchStaples(ctx, locationID)
}

func (p routingStaplesProvider) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]kroger.Ingredient, error) {
	provider, err := p.providerForLocation(locationID)
	if err != nil {
		return nil, err
	}
	return provider.GetIngredients(ctx, locationID, searchTerm, skip)
}

func (s *cachedStaplesService) GetStaples(ctx context.Context, p *GeneratorParams) ([]kroger.Ingredient, error) {
	lochash := p.LocationHash()

	if cachedIngredients, err := s.cache.IngredientsFromCache(ctx, lochash); err == nil {
		slog.InfoContext(ctx, "serving cached ingredients", "location", p.String(), "hash", lochash, "count", len(cachedIngredients))
		return cachedIngredients, nil
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to read cached ingredients", "location", p.String(), "error", err)
	}

	ingredients, err := s.provider.FetchStaples(ctx, p.Location.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingredients for staples for %s: %w", p.Location.ID, err)
	}
	ingredients = uniqueByDescription(ingredients)
	mutable.Shuffle(ingredients)

	if err := s.cache.SaveIngredients(ctx, lochash, ingredients); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	return ingredients, nil
}

func (s *cachedStaplesService) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int, date time.Time) ([]kroger.Ingredient, error) {
	cacheKey := wineIngredientsCacheKey(searchTerm, locationID, date)
	logger := slog.With("location", locationID, "date", date.Format("2006-01-02"), "style", searchTerm)

	wines, err := s.cache.IngredientsFromCache(ctx, cacheKey)
	if err == nil {
		logger.InfoContext(ctx, "serving cached ingredients", "count", len(wines))
		return wines, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		logger.ErrorContext(ctx, "failed to read cached ingredients", "error", err)
	}

	wines, err = s.provider.GetIngredients(ctx, locationID, searchTerm, skip)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingredients for %q: %w", searchTerm, err)
	}
	logger.InfoContext(ctx, "found ingredients", "count", len(wines))

	if err := s.cache.SaveIngredients(ctx, cacheKey, wines); err != nil {
		logger.ErrorContext(ctx, "failed to cache ingredients", "error", err)
	}
	return wines, nil
}

func (s *cachedStaplesService) Watchdog(ctx context.Context) error {
	storeIDs := []string{
		"wholefoods_10153",
		"safeway_490",
		"70500874",
		"starmarket_3566",
		"acmemarkets_806",
	}
	_, err := parallelism.Flatten(storeIDs, func(storeID string) ([]kroger.Ingredient, error) {
		return s.provider.FetchStaples(ctx, storeID)
	})
	return err
}

func staplesSignatureForLocation(locationID string) string {
	for _, provider := range defaultIdentityProviders() {
		if provider.IsID(locationID) {
			return provider.Signature()
		}
	}

	if testing.Testing() && locationID == "loc-123" {
		return kroger.NewIdentityProvider().Signature()
	}

	panic("unknown staples provider for location " + locationID)
}

func (p routingStaplesProvider) providerForLocation(locationID string) (backendStaplesProvider, error) {
	for _, backend := range p.backends {
		if backend.IsID(locationID) {
			return backend, nil
		}
	}
	return nil, fmt.Errorf("staples provider does not support location %q", locationID)
}

func defaultStaplesBackends(cfg *config.Config, krogerClient kroger.ClientWithResponsesInterface) ([]backendStaplesProvider, error) {
	// should we do this per request so we get new proxies per user? https://github.com/paulgmiller/careme/issues/443
	httpClient, err := brightdata.NewProxyAwareHTTPClient(cfg.BrightDataProxy)
	if err != nil {
		return nil, fmt.Errorf("create bright data proxy-aware client: %w", err)
	}

	// only returns an err because it ensures a cache for reese84 tokens.
	albertsonsProvider, err := albertsons.NewStaplesProvider(cfg.Albertsons, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create albertsons staples provider: %w", err)
	}

	return []backendStaplesProvider{
		kroger.NewStaplesProvider(krogerClient),
		albertsonsProvider,
		// actowiz.NewStaplesProvider(),
		walmart.NewStaplesProvider(),
		wholefoods.NewStaplesProvider(wholefoods.NewClient(httpClient)),
	}, nil
}

func defaultIdentityProviders() []identityProvider {
	return []identityProvider{
		kroger.NewIdentityProvider(),
		// actowiz.NewIdentityProvider(),
		albertsons.NewIdentityProvider(),
		wholefoods.NewIdentityProvider(),
		walmart.NewIdentityProvider(),
	}
}
