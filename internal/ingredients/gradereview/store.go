package gradereview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/ingredients/grading"
)

const reviewCachePrefix = "ingredient_grade_reviews/"

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
}

type Candidate struct {
	GradeKey   string
	Ingredient ai.InputIngredient
	Reviewed   int
	Total      int
}

type Store struct {
	cache cache.ListCache

	mu          sync.Mutex
	loaded      bool
	gradeKeys   []string
	gradeKeySet map[string]struct{}
	reviewed    map[string]struct{}
}

func NewStore(c cache.ListCache) *Store {
	if c == nil {
		panic("cache must not be nil")
	}
	return &Store{cache: c}
}

func ReviewCachePrefix() string {
	return reviewCachePrefix
}

func (s *Store) Next(ctx context.Context) (*Candidate, error) {
	if err := s.loadIndex(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	var nextKey string
	for _, key := range s.gradeKeys {
		if _, ok := s.reviewed[key]; ok {
			continue
		}
		nextKey = key
		break
	}
	reviewedCount := len(s.reviewed)
	total := len(s.gradeKeys)
	s.mu.Unlock()

	if nextKey == "" {
		return &Candidate{Reviewed: reviewedCount, Total: total}, nil
	}
	ingredient, err := s.loadIngredient(ctx, nextKey)
	if err != nil {
		return nil, err
	}
	return &Candidate{
		GradeKey:   nextKey,
		Ingredient: *ingredient,
		Reviewed:   reviewedCount,
		Total:      total,
	}, nil
}

func (s *Store) Save(ctx context.Context, gradeKey string, verdict Verdict, reviewedAt time.Time) error {
	if !verdict.Valid() {
		return ErrInvalidVerdict
	}
	if err := s.loadIndex(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	_, exists := s.gradeKeySet[gradeKey]
	_, alreadyReviewed := s.reviewed[gradeKey]
	s.mu.Unlock()
	if !exists {
		return cache.ErrNotFound
	}
	if alreadyReviewed {
		return cache.ErrAlreadyExists
	}

	ingredient, err := s.loadIngredient(ctx, gradeKey)
	if err != nil {
		return err
	}
	review := Review{
		GradeKey:   gradeKey,
		Ingredient: *ingredient,
		Verdict:    verdict,
		ReviewedAt: reviewedAt.UTC(),
	}
	body, err := json.Marshal(review)
	if err != nil {
		return fmt.Errorf("encode ingredient grade review: %w", err)
	}
	if err := s.cache.Put(ctx, reviewCachePrefix+gradeKey, string(body), cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			s.markReviewed(gradeKey)
		}
		return fmt.Errorf("save ingredient grade review: %w", err)
	}
	s.markReviewed(gradeKey)
	return nil
}

func (s *Store) loadIndex(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}

	gradeKeys, err := s.cache.List(ctx, grading.CachePrefix(), "")
	if err != nil {
		return fmt.Errorf("list ingredient grades: %w", err)
	}
	reviewKeys, err := s.cache.List(ctx, reviewCachePrefix, "")
	if err != nil {
		return fmt.Errorf("list ingredient grade reviews: %w", err)
	}

	gradeKeySet := make(map[string]struct{}, len(gradeKeys))
	for _, key := range gradeKeys {
		gradeKeySet[key] = struct{}{}
	}
	reviewed := make(map[string]struct{}, len(reviewKeys))
	for _, key := range reviewKeys {
		if _, ok := gradeKeySet[key]; ok {
			reviewed[key] = struct{}{}
		}
	}

	s.gradeKeys = gradeKeys
	s.gradeKeySet = gradeKeySet
	s.reviewed = reviewed
	s.loaded = true
	return nil
}

func (s *Store) markReviewed(gradeKey string) {
	s.mu.Lock()
	s.reviewed[gradeKey] = struct{}{}
	s.mu.Unlock()
}

func (v Verdict) Valid() bool {
	switch v {
	case VerdictTooHigh, VerdictCorrect, VerdictTooLow:
		return true
	default:
		return false
	}
}

func (s *Store) loadIngredient(ctx context.Context, gradeKey string) (*ai.InputIngredient, error) {
	reader, err := s.cache.Get(ctx, grading.CachePrefix()+gradeKey)
	if err != nil {
		return nil, fmt.Errorf("load ingredient grade %q: %w", gradeKey, err)
	}
	defer func() {
		_ = reader.Close()
	}()

	var ingredient ai.InputIngredient
	if err := json.NewDecoder(reader).Decode(&ingredient); err != nil {
		return nil, fmt.Errorf("decode ingredient grade %q: %w", gradeKey, err)
	}
	if ingredient.Grade == nil {
		return nil, fmt.Errorf("ingredient grade %q has no grade", gradeKey)
	}
	return &ingredient, nil
}
