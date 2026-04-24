package recipes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/albertsons"
	"careme/internal/brightdata"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/kroger"
	"careme/internal/parallelism"
	"careme/internal/walmart"
	"careme/internal/wholefoods"

	"github.com/samber/lo"
	"github.com/samber/lo/mutable"
)

// todo make this a indepenedent ingredient object not kroger.
// we're cheating and making the cache do the conversion for now but all underlying provider should create input ingredients
type krogerProvider interface {
	FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error)
	GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]kroger.Ingredient, error)
}

type identityProvider interface {
	IsID(locationID string) bool
	Signature() string
}

type backendStaplesProvider interface {
	identityProvider
	krogerProvider
}

type routingStaplesProvider struct {
	backends []backendStaplesProvider
}

func NewStaplesProvider(cfg *config.Config) (krogerProvider, error) {
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

type ingredientio interface {
	SaveIngredients(ctx context.Context, hash string, ingredients []ai.InputIngredient) error
	IngredientsFromCache(ctx context.Context, hash string) ([]ai.InputIngredient, error)
}

type cachedStaplesService struct {
	provider krogerProvider
	cache    ingredientio
	grader   ingredientgrading.Service
}

func NewCachedStaplesService(cfg *config.Config, c cache.Cache, grader ingredientgrading.Service) (*cachedStaplesService, error) {
	provider, err := NewStaplesProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create staples provider: %w", err)
	}
	rio := IO(c)
	return &cachedStaplesService{
		provider: provider,
		cache:    rio,
		grader:   grader,
	}, nil
}

func (s *cachedStaplesService) FetchStaples(ctx context.Context, p *GeneratorParams) ([]ai.InputIngredient, error) {
	lochash := p.LocationHash()

	if cachedIngredients, err := s.cache.IngredientsFromCache(ctx, lochash); err == nil {
		slog.InfoContext(ctx, "serving cached ingredients", "location", p.String(), "hash", lochash, "count", len(cachedIngredients))
		return cachedIngredients, nil
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to read cached ingredients", "location", p.String(), "error", err)
	}

	// this does grading
	ingredients, err := s.provider.FetchStaples(ctx, p.Location.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingredients for staples for %s: %w", p.Location.ID, err)
	}
	ingredients = lo.UniqBy(ingredients, func(i kroger.Ingredient) string {
		return *i.ProductId
	})
	mutable.Shuffle(ingredients)

	// does this belong hehe?
	inputs, err := s.gradeIngredients(ctx, ingredients)
	if err != nil {
		return nil, err
	}

	if err := s.cache.SaveIngredients(ctx, lochash, inputs); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	return inputs, nil
}

// this is not actually wine specificexcept that GetIngredients only does wine requests from ui
// command line could still call it kroger style.
func wineIngredientsCacheKey(style, location string, date time.Time) string {
	normalizedStyle := strings.ToLower(strings.TrimSpace(style))
	fnv := fnv.New64a()
	lo.Must(io.WriteString(fnv, location))
	lo.Must(io.WriteString(fnv, date.Format("2006-01-02")))
	lo.Must(io.WriteString(fnv, normalizedStyle))
	return "wines/" + base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

func (s *cachedStaplesService) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int, date time.Time) ([]ai.InputIngredient, error) {
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

	krogerwines, err := s.provider.GetIngredients(ctx, locationID, searchTerm, skip)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingredients for %q: %w", searchTerm, err)
	}
	wines = []ai.InputIngredient{}
	for _, kwine := range krogerwines {
		wine, err := ingredientgrading.InputIngredientFromKrogerIngredient(kwine)
		if err != nil {
			return nil, err
		}
		wines = append(wines, wine)
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

// this shold go away when everyone returns inputingredients
func (s *cachedStaplesService) gradeIngredients(ctx context.Context, ingredients []kroger.Ingredient) ([]ai.InputIngredient, error) {
	if s.grader != nil {
		graded, err := s.grader.GradeIngredients(ctx, ingredients)
		if err == nil {
			return graded, nil
		}
		slog.ErrorContext(ctx, "failed to grade cached staples", "error", err)
	}

	inputs := make([]ai.InputIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		input, err := ingredientgrading.InputIngredientFromKrogerIngredient(ingredient)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
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
