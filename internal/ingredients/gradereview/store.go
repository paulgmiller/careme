package gradereview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/ingredients/cachekey"
	"careme/internal/locations"
)

const reviewCachePrefix = "ingredient_grade_reviews/"

// Catalog reviews identify the ingredient and displayed score, independently of
// the grading manager or its model-specific cache version.
const catalogReviewVersion = "catalog-v1"

type verdict string

const (
	verdictTooHigh verdict = "too_high"
	verdictCorrect verdict = "correct"
	verdictTooLow  verdict = "too_low"
)

var errInvalidVerdict = errors.New("invalid ingredient grade verdict")

type reviewRecord struct {
	GradeKey   string             `json:"grade_key"`
	Ingredient ai.InputIngredient `json:"ingredient"`
	Verdict    verdict            `json:"verdict"`
	ReviewedAt time.Time          `json:"reviewed_at"`
}

type CatalogCandidate struct {
	GradeKey   string
	Ingredient ai.InputIngredient
}

type locationLookup interface {
	GetLocationByID(context.Context, string) (*locations.Location, error)
}

type ingredientCache interface {
	IngredientsFromCache(context.Context, string) ([]ai.InputIngredient, error)
}

const catalogTTL = 30 * time.Minute

type catalogEntry struct {
	location    *locations.Location
	ingredients []ai.InputIngredient
}

func (e *catalogEntry) remove(i int) {
	e.ingredients = slices.Delete(e.ingredients, i, i+1)
	if len(e.ingredients) == 0 {
		e.ingredients = nil
	}
}

type store struct {
	mu          sync.Mutex
	cache       cache.Cache
	entries     map[string]*catalogEntry
	locations   locationLookup
	ingredients ingredientCache
	now         func() time.Time
}

func NewStore(c cache.Cache, locations locationLookup, ingredients ingredientCache) *store {
	if c == nil {
		panic("cache must not be nil")
	}
	return &store{cache: c, locations: locations, ingredients: ingredients, now: time.Now, entries: make(map[string]*catalogEntry)}
}

func (s *store) saveReview(ctx context.Context, review reviewRecord) error {
	body, err := json.Marshal(review)
	if err != nil {
		return fmt.Errorf("encode ingredient grade review: %w", err)
	}
	if err := s.cache.Put(ctx, reviewCachePrefix+review.GradeKey, string(body), cache.IfNoneMatch()); err != nil {
		return fmt.Errorf("save ingredient grade review: %w", err)
	}
	return nil
}

func (v verdict) valid() bool {
	switch v {
	case verdictTooHigh, verdictCorrect, verdictTooLow:
		return true
	default:
		return false
	}
}

// Next removes previously reviewed ingredients from the chosen store and selects
// its next unreviewed ingredient.
func (s *store) Next(ctx context.Context, locationID string) (*locations.Location, *CatalogCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.loadCatalog(ctx, locationID)
	if err != nil {
		return nil, nil, err
	}
	for len(entry.ingredients) > 0 {
		ingredient := entry.ingredients[0]
		if ingredient.Grade == nil {
			entry.remove(0)
			continue
		}
		key := catalogGradeKey(ingredient)
		reviewed, err := s.cache.Exists(ctx, reviewCachePrefix+key)
		if err != nil {
			return nil, nil, fmt.Errorf("check ingredient grade review %q: %w", key, err)
		}
		if !reviewed {
			return entry.location, &CatalogCandidate{GradeKey: key, Ingredient: ingredient}, nil
		}
		entry.remove(0)
	}
	return entry.location, &CatalogCandidate{}, nil
}

// save validates membership and persists the server-side snapshot.
func (s *store) save(ctx context.Context, locationID, gradeKey string, verdict verdict, reviewedAt time.Time) error {
	if !verdict.valid() {
		return errInvalidVerdict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, err := s.loadCatalog(ctx, locationID)
	if err != nil {
		return err
	}
	for i, ingredient := range entry.ingredients {
		if ingredient.Grade == nil || catalogGradeKey(ingredient) != gradeKey {
			continue
		}
		ingredient.Embedding = nil
		err := s.saveReview(ctx, reviewRecord{GradeKey: gradeKey, Ingredient: ingredient, Verdict: verdict, ReviewedAt: reviewedAt.UTC()})
		if err == nil || errors.Is(err, cache.ErrAlreadyExists) {
			entry.remove(i)
		}
		return err
	}
	return cache.ErrNotFound
}

func catalogGradeKey(ingredient ai.InputIngredient) string {
	return fmt.Sprintf("%s/%s/%d", catalogReviewVersion, ai.NormalizeInputIngredient(ingredient).Hash(), ingredient.Grade.Score)
}

// loadCatalog is called with mu held. Catalogs expire 30 minutes after loading.
func (s *store) loadCatalog(ctx context.Context, id string) (*catalogEntry, error) {
	if entry := s.entries[id]; entry != nil {
		return entry, nil
	}
	location, err := s.locations.GetLocationByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load store %q: %w", id, err)
	}
	date, err := locations.StoreTime(ctx, s.now(), location)
	if err != nil {
		return nil, fmt.Errorf("resolve store date: %w", err)
	}
	for _, day := range []time.Time{date, date.AddDate(0, 0, -1)} {
		ingredients, err := s.ingredients.IngredientsFromCache(ctx, cachekey.ForStore(location.ID, day))
		if err == nil {
			entry := &catalogEntry{location: location, ingredients: ingredients}
			s.entries[id] = entry
			time.AfterFunc(catalogTTL, func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				delete(s.entries, id)
			})
			return entry, nil
		}
		if !errors.Is(err, cache.ErrNotFound) {
			return nil, fmt.Errorf("load cached catalog for %q: %w", id, err)
		}
	}
	return nil, fmt.Errorf("load cached catalog for %q: %w", id, cache.ErrNotFound)
}
