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
// we're cheating and making the wrapper here  do the conversion for now but all underlying provider should create input ingredients
// then this becomes staplesProvider
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

func NewStaplesProvider(cfg *config.Config) (staplesProvider, error) {
	kclient, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	backends, err := defaultStaplesBackends(cfg, kclient)
	if err != nil {
		return nil, err
	}

	return convertingProvider{routingStaplesProvider{
		backends: backends,
	}}, nil
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

type grader interface {
	GradeIngredients(ctx context.Context, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error)
}

type cachedStaplesService struct {
	provider staplesProvider
	cache    ingredientio
	grader   grader
}

type staplesProvider interface {
	FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error)
	GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]ai.InputIngredient, error)
}

type convertingProvider struct {
	kprovider krogerProvider
}

var _ staplesProvider = convertingProvider{}

func (cp convertingProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	ingredients, err := cp.kprovider.FetchStaples(ctx, locationID)
	if err != nil {
		return nil, err
	}
	inputs := make([]ai.InputIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		input, err := ingredientgrading.InputIngredientFromKrogerIngredient(ingredient)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}

	inputs = lo.UniqBy(inputs, func(i ai.InputIngredient) string {
		return i.ProductID
	})
	return inputs, nil
}

func (cp convertingProvider) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]ai.InputIngredient, error) {
	ingredients, err := cp.kprovider.GetIngredients(ctx, locationID, searchTerm, skip)
	if err != nil {
		return nil, err
	}
	inputs := make([]ai.InputIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		input, err := ingredientgrading.InputIngredientFromKrogerIngredient(ingredient)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}

	inputs = lo.UniqBy(inputs, func(i ai.InputIngredient) string {
		return i.ProductID
	})
	return inputs, nil
}

func NewCachedStaplesService(cfg *config.Config, c cache.Cache, grader grader) (*cachedStaplesService, error) {
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
	locationID := p.Location.ID

	cachedIngredients, err := s.cache.IngredientsFromCache(ctx, lochash)
	if err == nil {
		slog.InfoContext(ctx, "serving cached ingredients", "location", locationID, "hash", lochash, "count", len(cachedIngredients))
		// do we still want this randomness after grading?
		mutable.Shuffle(cachedIngredients)
		return s.grader.GradeIngredients(ctx, cachedIngredients)
		// shoulld we save?
	}

	if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to read cached ingredients", "location", locationID, "error", err)
	}

	ingredients, err := s.provider.FetchStaples(ctx, locationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingredients for staples for %s: %w", locationID, err)
	}

	graded, err := s.grader.GradeIngredients(ctx, ingredients)
	if err != nil {
		slog.ErrorContext(ctx, "failed to grade cached staples", "error", err)
		return nil, err
	}
	// do we still want this randomness after grading?
	mutable.Shuffle(graded)

	if err := s.cache.SaveIngredients(ctx, lochash, graded); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	return graded, nil
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
	_, err := parallelism.Flatten(storeIDs, func(storeID string) ([]ai.InputIngredient, error) {
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
