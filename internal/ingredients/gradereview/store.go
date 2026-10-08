package gradereview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
)

const reviewCachePrefix = "ingredient_grade_reviews/"

// Catalog reviews identify the ingredient and displayed score, independently of
// the grading manager or its model-specific cache version.
const catalogReviewVersion = "catalog-v1"

type Verdict string

const (
	VerdictTooHigh Verdict = "too_high"
	VerdictCorrect Verdict = "correct"
	VerdictTooLow  Verdict = "too_low"
)

var ErrInvalidVerdict = errors.New("invalid ingredient grade verdict")

type Review struct {
	GradeKey   string             `json:"grade_key"`
	Ingredient ai.InputIngredient `json:"ingredient"`
	Verdict    Verdict            `json:"verdict"`
	ReviewedAt time.Time          `json:"reviewed_at"`
	LocationID string             `json:"location_id,omitempty"`
}

type Candidate struct {
	GradeKey   string
	Ingredient ai.InputIngredient
}

type Store struct{ cache cache.Cache }

func NewStore(c cache.Cache) *Store {
	if c == nil {
		panic("cache must not be nil")
	}
	return &Store{cache: c}
}

func (s *Store) saveReview(ctx context.Context, review Review) error {
	body, err := json.Marshal(review)
	if err != nil {
		return fmt.Errorf("encode ingredient grade review: %w", err)
	}
	if err := s.cache.Put(ctx, reviewCachePrefix+review.GradeKey, string(body), cache.IfNoneMatch()); err != nil {
		return fmt.Errorf("save ingredient grade review: %w", err)
	}
	return nil
}

func (v Verdict) Valid() bool {
	switch v {
	case VerdictTooHigh, VerdictCorrect, VerdictTooLow:
		return true
	default:
		return false
	}
}

// NextFromCatalog selects an unreviewed ingredient from the chosen store only.
func (s *Store) NextFromCatalog(ctx context.Context, ingredients []ai.InputIngredient) (*Candidate, error) {
	for _, ingredient := range ingredients {
		if ingredient.Grade == nil {
			continue
		}
		key := s.catalogGradeKey(ingredient)
		reviewed, err := s.cache.Exists(ctx, reviewCachePrefix+key)
		if err != nil {
			return nil, fmt.Errorf("check ingredient grade review %q: %w", key, err)
		}
		if reviewed {
			continue
		}
		return &Candidate{GradeKey: key, Ingredient: ingredient}, nil
	}
	return &Candidate{}, nil
}

// SaveFromCatalog validates membership and persists the server-side snapshot.
func (s *Store) SaveFromCatalog(ctx context.Context, locationID, gradeKey string, ingredients []ai.InputIngredient, verdict Verdict, reviewedAt time.Time) error {
	if !verdict.Valid() {
		return ErrInvalidVerdict
	}
	for _, ingredient := range ingredients {
		if ingredient.Grade == nil || s.catalogGradeKey(ingredient) != gradeKey {
			continue
		}
		ingredient.Embedding = nil
		return s.saveReview(ctx, Review{GradeKey: gradeKey, Ingredient: ingredient, Verdict: verdict, ReviewedAt: reviewedAt.UTC(), LocationID: locationID})
	}
	return cache.ErrNotFound
}

func (s *Store) catalogGradeKey(ingredient ai.InputIngredient) string {
	return fmt.Sprintf("%s/%s/%d", catalogReviewVersion, ai.NormalizeInputIngredient(ingredient).Hash(), ingredient.Grade.Score)
}
