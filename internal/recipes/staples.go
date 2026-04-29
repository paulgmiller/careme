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
	"careme/internal/kroger"
	"careme/internal/parallelism"
	"careme/internal/telemetry"
	"careme/internal/walmart"
	"careme/internal/wholefoods"

	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
)

type identityProvider interface {
	IsID(locationID string) bool
	Signature() string
}

type backendStaplesProvider interface {
	identityProvider
	staplesProvider
}

// sends to the right backend but also dedupes product ids and errors on empty ones.
type routingStaplesProvider struct {
	backends []backendStaplesProvider
}

func NewStaplesProvider(cfg *config.Config) (staplesProvider, error) {
	backends, err := defaultStaplesBackends(cfg)
	if err != nil {
		return nil, err
	}

	return routingStaplesProvider{
		backends: backends,
	}, nil
}

func (p routingStaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	provider, err := p.providerForLocation(locationID)
	if err != nil {
		return nil, err
	}
	ingredients, err := provider.FetchStaples(ctx, locationID)
	if err != nil {
		return nil, err
	}
	return dedupeInputIngredients(ingredients)
}

func (p routingStaplesProvider) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]ai.InputIngredient, error) {
	provider, err := p.providerForLocation(locationID)
	if err != nil {
		return nil, err
	}
	ingredients, err := provider.GetIngredients(ctx, locationID, searchTerm, skip)
	if err != nil {
		return nil, err
	}
	return dedupeInputIngredients(ingredients)
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

func dedupeInputIngredients(ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	seen := map[string]bool{}
	var deduped []ai.InputIngredient
	for _, ingredient := range ingredients {
		if ingredient.ProductID == "" {
			return nil, fmt.Errorf("blank product id for ingredient: %+v", ingredient)
		}
		if seen[ingredient.ProductID] {
			continue
		}
		seen[ingredient.ProductID] = true
		deduped = append(deduped, ingredient)
	}
	return deduped, nil
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

func (s *cachedStaplesService) FetchStaples(ctx context.Context, p *GeneratorParams) (results []ai.InputIngredient, err error) {
	lochash := p.LocationHash()
	locationID := p.Location.ID
	ctx, span := telemetry.Start(ctx, "careme/internal/recipes", "recipes.staples.fetch")
	defer telemetry.End(span, &err)
	span.SetAttributes(attribute.String("location.provider", safeStaplesSignatureForLocation(locationID)))

	cachedIngredients, err := s.cache.IngredientsFromCache(ctx, lochash)
	if err == nil {
		slog.InfoContext(ctx, "serving cached ingredients", "location", locationID, "hash", lochash, "count", len(cachedIngredients))
		span.SetAttributes(attribute.Bool("cache.hit", true), attribute.Int("staples.cached_count", len(cachedIngredients)))
		return s.grader.GradeIngredients(ctx, cachedIngredients)
		// shoulld we save?
	}
	span.SetAttributes(attribute.Bool("cache.hit", false))

	if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to read cached ingredients", "location", locationID, "error", err)
	}

	providerCtx, providerSpan := telemetry.Start(ctx, "careme/internal/recipes", "recipes.staples.provider_fetch")
	ingredients, err := s.provider.FetchStaples(providerCtx, locationID)
	telemetry.EndResult(providerSpan, err)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingredients for staples for %s: %w", locationID, err)
	}
	span.SetAttributes(attribute.Int("staples.provider_count", len(ingredients)))

	graded, err := s.grader.GradeIngredients(ctx, ingredients)
	if err != nil {
		slog.ErrorContext(ctx, "failed to grade cached staples", "error", err)
		return nil, err
	}

	if err := s.cache.SaveIngredients(ctx, lochash, graded); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	span.SetAttributes(attribute.Int("staples.result_count", len(graded)))
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

func (s *cachedStaplesService) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int, date time.Time) (wines []ai.InputIngredient, err error) {
	cacheKey := wineIngredientsCacheKey(searchTerm, locationID, date)
	logger := slog.With("location", locationID, "date", date.Format("2006-01-02"), "style", searchTerm)
	ctx, span := telemetry.Start(ctx, "careme/internal/recipes", "recipes.ingredients.lookup")
	defer telemetry.End(span, &err)
	span.SetAttributes(
		attribute.String("location.provider", safeStaplesSignatureForLocation(locationID)),
		attribute.Int("ingredients.skip", skip),
	)

	wines, err = s.cache.IngredientsFromCache(ctx, cacheKey)
	if err == nil {
		logger.InfoContext(ctx, "serving cached ingredients", "count", len(wines))
		span.SetAttributes(attribute.Bool("cache.hit", true), attribute.Int("ingredients.result_count", len(wines)))
		return wines, nil
	}
	span.SetAttributes(attribute.Bool("cache.hit", false))
	if !errors.Is(err, cache.ErrNotFound) {
		logger.ErrorContext(ctx, "failed to read cached ingredients", "error", err)
	}

	providerCtx, providerSpan := telemetry.Start(ctx, "careme/internal/recipes", "recipes.ingredients.provider_lookup")
	wines, err = s.provider.GetIngredients(providerCtx, locationID, searchTerm, skip)
	telemetry.EndResult(providerSpan, err)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingredients for %q: %w", searchTerm, err)
	}
	logger.InfoContext(ctx, "found ingredients", "count", len(wines))
	span.SetAttributes(attribute.Int("ingredients.result_count", len(wines)))

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

func defaultStaplesBackends(cfg *config.Config) ([]backendStaplesProvider, error) {
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

	krogerBackend, err := kroger.NewStaplesProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kroger staples provider: %w", err)
	}

	return []backendStaplesProvider{
		albertsonsProvider,
		krogerBackend,
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
