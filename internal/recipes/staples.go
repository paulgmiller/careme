package recipes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/parallelism"

	"github.com/samber/lo"
	"go.opentelemetry.io/otel/attribute"
)

// StaplesBackend is the provider contract consumed by recipe sourcing.
type StaplesBackend interface {
	IsID(string) bool
	FetchStaples(context.Context, string) ([]ai.InputIngredient, error)
	FetchWines(context.Context, string, []string) ([]ai.InputIngredient, error)
}

type backendStaplesProvider = StaplesBackend

type routingStaplesProvider struct {
	backends []backendStaplesProvider
}

type dedupingStaplesProvider struct {
	provider staplesProvider
}

func NewStaplesProvider(backends []StaplesBackend) staplesProvider {
	return dedupingStaplesProvider{provider: routingStaplesProvider{
		backends: backends,
	}}
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

func (p routingStaplesProvider) FetchPantry(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	provider, err := p.providerForLocation(locationID)
	if err != nil {
		return nil, err
	}
	pantry, ok := provider.(pantryProvider)
	if !ok {
		return nil, nil
	}
	return pantry.FetchPantry(ctx, locationID)
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

func (p dedupingStaplesProvider) FetchPantry(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	pantry, ok := p.provider.(pantryProvider)
	if !ok {
		return nil, nil
	}
	ingredients, err := pantry.FetchPantry(ctx, locationID)
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

type locationByID interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type staplesFetcher interface {
	FetchStaples(ctx context.Context, p *GeneratorParams) ([]ai.InputIngredient, error)
}

// wineCatalogProvider supplies a store-wide supplement to recipe-style searches.
type wineCatalogProvider interface {
	IsID(string) bool
	FetchWineCatalog(context.Context, string) ([]ai.InputIngredient, error)
}

type cachedStaplesService struct {
	wineCatalogProviders []wineCatalogProvider
	provider             staplesProvider
	cache                ingredientio
	grader               grader
}

type staplesProvider interface {
	FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error)
	FetchWines(ctx context.Context, locationID string, styles []string) ([]ai.InputIngredient, error)
}

type pantryProvider interface {
	FetchPantry(ctx context.Context, locationID string) ([]ai.InputIngredient, error)
}

// we drop extra categories here.
// TODO(#1051): Revisit deduplication by ProductID: keeping the first row also
// discards other variants. Selection may need to account for inventory or size.
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

func NewCachedStaplesService(backends []StaplesBackend, c cache.Cache, grader grader) *cachedStaplesService {
	provider := NewStaplesProvider(backends)
	var catalogs []wineCatalogProvider
	for _, backend := range backends {
		if catalog, ok := backend.(wineCatalogProvider); ok {
			catalogs = append(catalogs, catalog)
		}
	}
	rio := IO(c)
	return &cachedStaplesService{
		provider:             provider,
		wineCatalogProviders: catalogs,
		cache:                rio,
		grader:               grader,
	}
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
	slog.InfoContext(ctx, "cached ingredients", "location", p.Location.ID, "date", p.Date.Format("2006-01-02"), "hash", lochash, "count", len(graded))
	// "produce_score", sumIngredientGradesAboveCutoff(graded))
	return graded, nil
}

// FetchPantry loads the independently cached pantry catalog for a store.
func (s *cachedStaplesService) FetchPantry(ctx context.Context, p *GeneratorParams) ([]ai.InputIngredient, error) {
	key := "pantry/query-categories-v1/" + p.LocationHash()
	if cached, err := s.cache.IngredientsFromCache(ctx, key); err == nil {
		return s.grader.GradeIngredients(ctx, cached)
	} else if !errors.Is(err, cache.ErrNotFound) {
		return nil, fmt.Errorf("load cached pantry: %w", err)
	}
	provider, ok := s.provider.(pantryProvider)
	if !ok {
		return nil, fmt.Errorf("pantry provider is unavailable for %s", p.Location.ID)
	}
	pantry, err := provider.FetchPantry(ctx, p.Location.ID)
	if err != nil {
		return nil, fmt.Errorf("fetch pantry for %s: %w", p.Location.ID, err)
	}
	pantry, err = s.grader.GradeIngredients(ctx, pantry)
	if err != nil {
		return nil, fmt.Errorf("grade pantry for %s: %w", p.Location.ID, err)
	}
	if err := s.cache.SaveIngredients(ctx, key, pantry); err != nil {
		return nil, fmt.Errorf("cache pantry for %s: %w", p.Location.ID, err)
	}
	return pantry, nil
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
	key := wineStylesCacheKey(styles, locationID, date)
	for _, provider := range s.wineCatalogProviders {
		if provider.IsID(locationID) {
			return s.fetchWinesWithCatalog(ctx, provider, locationID, styles, date, key)
		}
	}
	return s.cachedWines(ctx, key, func() ([]ai.InputIngredient, error) {
		return s.provider.FetchWines(ctx, locationID, styles)
	})
}

func (s *cachedStaplesService) fetchWinesWithCatalog(ctx context.Context, provider wineCatalogProvider, locationID string, styles []string, date time.Time, key string) ([]ai.InputIngredient, error) {
	// Keep query caches separate from the combined snapshot used by shopping finalization.
	catalogKey := "wines/catalog-red-white-v1/" + wineIngredientsCacheKey("", locationID, date)
	searchKey := "wines/style-search-v1/" + key
	wines, err := parallelism.Flatten([]func() ([]ai.InputIngredient, error){
		func() ([]ai.InputIngredient, error) {
			return s.cachedWines(ctx, searchKey, func() ([]ai.InputIngredient, error) {
				return s.provider.FetchWines(ctx, locationID, styles)
			})
		},
		func() ([]ai.InputIngredient, error) {
			return s.cachedWines(ctx, catalogKey, func() ([]ai.InputIngredient, error) {
				return provider.FetchWineCatalog(ctx, locationID)
			})
		},
	}, func(fetch func() ([]ai.InputIngredient, error)) ([]ai.InputIngredient, error) { return fetch() })
	if err != nil {
		return nil, err
	}
	wines, err = dedupeInputIngredients(wines)
	if err != nil {
		return nil, err
	}
	if err := s.cache.SaveIngredients(ctx, key, wines); err != nil {
		return nil, fmt.Errorf("cache combined wines for %s: %w", locationID, err)
	}
	return wines, nil
}

func (s *cachedStaplesService) cachedWines(ctx context.Context, key string, fetch func() ([]ai.InputIngredient, error)) ([]ai.InputIngredient, error) {
	wines, err := s.cache.IngredientsFromCache(ctx, key)
	if err == nil {
		return wines, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return nil, fmt.Errorf("read wine candidates %s: %w", key, err)
	}
	wines, err = fetch()
	if err != nil {
		return nil, fmt.Errorf("fetch wine candidates %s: %w", key, err)
	}
	if err := s.cache.SaveIngredients(ctx, key, wines); err != nil {
		return nil, fmt.Errorf("cache wine candidates %s: %w", key, err)
	}
	return wines, nil
}

type staplesWatchdog struct {
	locations locationByID
	staples   staplesFetcher
}

func NewStaplesWatchdog(locations locationByID, staples staplesFetcher) *staplesWatchdog {
	return &staplesWatchdog{locations: locations, staples: staples}
}

func (w *staplesWatchdog) Watchdog(ctx context.Context) error {
	_, err := parallelism.MapWithErrors(StaplesWatchdogLocationIDs(), func(locationID string) (int, error) {
		store, err := w.locations.GetLocationByID(ctx, locationID)
		if err != nil {
			return 0, err
		}
		date, err := locations.StoreTime(ctx, nowFn(), store)
		if err != nil {
			return 0, err
		}
		_, err = w.staples.FetchStaples(ctx, DefaultParams(store, date))
		return 0, err
	})
	return err
}

func StaplesWatchdogLocationIDs() []string {
	return []string{
		"wholefoods_10153",
		"safeway_490",
		"70500874",
		"starmarket_3566",
		"acmemarkets_806",
		"publix_1847",
		"aldi_F219",
		"heb_540",
	}
}

func (p routingStaplesProvider) providerForLocation(locationID string) (backendStaplesProvider, error) {
	for _, backend := range p.backends {
		if backend.IsID(locationID) {
			return backend, nil
		}
	}
	return nil, fmt.Errorf("staples provider does not support location %q", locationID)
}
