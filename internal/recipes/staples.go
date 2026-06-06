package recipes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/albertsons"
	"careme/internal/aldi"
	"careme/internal/brightdata"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/heb"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/parallelism"
	"careme/internal/publix"
	"careme/internal/walmart"
	"careme/internal/wholefoods"

	"github.com/samber/lo"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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

type routingStaplesProvider struct {
	backends []backendStaplesProvider
}

type dedupingStaplesProvider struct {
	provider staplesProvider
}

func NewStaplesProvider(cfg *config.Config) (staplesProvider, error) {
	backends, err := defaultStaplesBackends(cfg)
	if err != nil {
		return nil, err
	}

	return dedupingStaplesProvider{provider: routingStaplesProvider{
		backends: backends,
	}}, nil
}

func (p routingStaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	provider, err := p.providerForLocation(locationID)
	if err != nil {
		return nil, err
	}
	ctx, span := tracer.Start(ctx, "staples.fetchstaples")
	span.SetAttributes(attribute.String("backend", fmt.Sprintf("%T", provider)))
	defer span.End()
	return provider.FetchStaples(ctx, locationID)
}

func (p routingStaplesProvider) FetchWines(ctx context.Context, locationID string, styles []string) ([]ai.InputIngredient, error) {
	provider, err := p.providerForLocation(locationID)
	if err != nil {
		return nil, err
	}
	ctx, span := tracer.Start(ctx, "staples.fetchwines")
	span.SetAttributes(attribute.String("backend", fmt.Sprintf("%T", provider)))
	defer span.End()
	return provider.FetchWines(ctx, locationID, styles)
}

func (p dedupingStaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	ingredients, err := p.provider.FetchStaples(ctx, locationID)
	if err != nil {
		return nil, err
	}
	return dedupeInputIngredients(ingredients)
}

func (p dedupingStaplesProvider) FetchWines(ctx context.Context, locationID string, styles []string) ([]ai.InputIngredient, error) {
	ingredients, err := p.provider.FetchWines(ctx, locationID, styles)
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
	FetchWines(ctx context.Context, locationID string, styles []string) ([]ai.InputIngredient, error)
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

func (s *cachedStaplesService) FetchStaples(ctx context.Context, p *GeneratorParams) ([]ai.InputIngredient, error) {
	lochash := p.LocationHash()
	locationID := p.Location.ID

	cachedIngredients, err := s.cache.IngredientsFromCache(ctx, lochash)
	if err == nil {
		slog.InfoContext(ctx, "serving cached ingredients", "location", locationID, "hash", lochash, "count", len(cachedIngredients))
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

	ctx, span := tracer.Start(ctx, "staples.gradeingredients")
	defer span.End()
	graded, err := s.grader.GradeIngredients(ctx, ingredients)
	if err != nil {
		slog.ErrorContext(ctx, "failed to grade cached staples", "error", err)
		return nil, err
	}

	if err := s.cache.SaveIngredients(ctx, lochash, graded); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	slog.InfoContext(ctx, "cached ingredients", "location", p.Location.ID, "date", p.Date.Format("2006-01-02"), "hash", lochash, "count", len(graded), "produce_score", sumIngredientGradesAboveCutoff(graded))
	return graded, nil
}

func wineIngredientsCacheKey(style, location string, date time.Time) string {
	normalizedStyle := strings.ToLower(strings.TrimSpace(style))
	fnv := fnv.New64a()
	lo.Must(io.WriteString(fnv, location))
	lo.Must(io.WriteString(fnv, date.Format("2006-01-02")))
	lo.Must(io.WriteString(fnv, normalizedStyle))
	return "wines/" + base64.RawURLEncoding.EncodeToString(fnv.Sum(nil))
}

func wineStylesCacheKey(styles []string, location string, date time.Time) string {
	normalized := normalizedWineStyles(styles)
	if len(normalized) == 1 {
		return wineIngredientsCacheKey(normalized[0], location, date)
	}
	return wineIngredientsCacheKey(strings.Join(normalized, "\t"), location, date)
}

func normalizedWineStyles(styles []string) []string {
	seen := map[string]bool{}
	var normalized []string
	for _, style := range styles {
		style = strings.TrimSpace(style)
		if style == "" {
			continue
		}
		key := strings.ToLower(style)
		if seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, style)
	}
	slices.SortFunc(normalized, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return normalized
}

func (s *cachedStaplesService) FetchWines(ctx context.Context, locationID string, styles []string, date time.Time) ([]ai.InputIngredient, error) {
	styles = normalizedWineStyles(styles)
	if len(styles) == 0 {
		return nil, nil
	}
	// TODO: Let providers normalize wine cache styles. Whole Foods ignores recipe
	// styles and always fetches red-wine/white-wine/sparkling, so style-based keys
	// currently create duplicate cache entries for the same candidate set.
	cacheKey := wineStylesCacheKey(styles, locationID, date)
	logger := slog.With("location", locationID, "date", date.Format("2006-01-02"), "styles", styles)

	wines, err := s.cache.IngredientsFromCache(ctx, cacheKey)
	if err == nil {
		logger.InfoContext(ctx, "serving cached wines", "count", len(wines))
		return wines, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		logger.ErrorContext(ctx, "failed to read cached wines", "error", err)
	}

	wines, err = s.provider.FetchWines(ctx, locationID, styles)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch wines for %q: %w", strings.Join(styles, ", "), err)
	}
	logger.InfoContext(ctx, "found wines", "count", len(wines))

	if err := s.cache.SaveIngredients(ctx, cacheKey, wines); err != nil {
		logger.ErrorContext(ctx, "failed to cache wines", "error", err)
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
		"publix_1847",
		"aldi_F219",
	}
	_, err := parallelism.Flatten(storeIDs, func(storeID string) ([]ai.InputIngredient, error) {
		return s.FetchStaples(ctx, DefaultParams(&locations.Location{ID: storeID}, watchdogDate(nowFn())))
	})
	return err
}

func watchdogDate(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
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

// should we pass in a wrapper/roundtripper
func defaultStaplesBackends(cfg *config.Config) ([]backendStaplesProvider, error) {
	// should we do this per request so we get new proxies per user? https://github.com/paulgmiller/careme/issues/443
	brightdataClient, err := brightdata.NewProxyAwareHTTPClient(cfg.BrightDataProxy)
	if err != nil {
		return nil, fmt.Errorf("create bright data proxy-aware client: %w", err)
	}
	brightdataClient.Transport = otelhttp.NewTransport(brightdataClient.Transport)

	// only returns an err because it ensures a cache for reese84 tokens.
	albertsonsProvider, err := albertsons.NewStaplesProvider(cfg.Albertsons, brightdataClient)
	if err != nil {
		return nil, fmt.Errorf("create albertsons staples provider: %w", err)
	}

	publixProvider, err := publix.NewStaplesProvider(cfg.Publix, brightdataClient)
	if err != nil {
		return nil, fmt.Errorf("create publix staples provider: %w", err)
	}

	hebProvider, err := heb.NewStaplesProvider(brightdataClient)
	if err != nil {
		return nil, fmt.Errorf("create heb staples provider: %w", err)
	}
	aldiProvider, err := aldi.NewStaplesProvider(brightdataClient)
	if err != nil {
		return nil, fmt.Errorf("create ALDI staples provider: %w", err)
	}

	// Kroger is a public API integration, not a scraper. Keep it off Bright Data;
	// retries are added in the Kroger client.
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	krogerBackend, err := kroger.NewStaplesProvider(cfg, httpClient)
	if err != nil {
		return nil, fmt.Errorf("create kroger staples provider: %w", err)
	}

	return []backendStaplesProvider{
		albertsonsProvider,
		hebProvider,
		aldiProvider,
		krogerBackend,
		publixProvider,
		// actowiz.NewStaplesProvider(),
		walmart.NewStaplesProvider(),
		wholefoods.NewStaplesProvider(wholefoods.NewClient(brightdataClient)),
	}, nil
}

func defaultIdentityProviders() []identityProvider {
	return []identityProvider{
		kroger.NewIdentityProvider(),
		// actowiz.NewIdentityProvider(),
		albertsons.NewIdentityProvider(),
		heb.NewIdentityProvider(),
		aldi.NewIdentityProvider(),
		publix.NewIdentityProvider(),
		wholefoods.NewIdentityProvider(),
		walmart.NewIdentityProvider(),
	}
}
