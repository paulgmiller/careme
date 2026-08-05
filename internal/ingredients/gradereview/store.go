package gradereview

import (
	"context"
	"crypto/rand"
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

const prefixAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

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
	prefix      func() (string, error)
}

func NewStore(c cache.ListCache) *Store {
	if c == nil {
		panic("cache must not be nil")
	}
	return &Store{cache: c, prefix: randomPrefix}
}

func ReviewCachePrefix() string {
	return reviewCachePrefix
}

func (s *Store) Next(ctx context.Context) (*Candidate, error) {
	if err := s.loadIndex(ctx); err != nil {
		return nil, err
	}

	candidate := s.nextCandidate()
	if candidate.GradeKey == "" && candidate.Total > 0 {
		s.mu.Lock()
		s.loaded = false
		s.mu.Unlock()
		if err := s.loadIndex(ctx); err != nil {
			return nil, err
		}
		candidate = s.nextCandidate()
	}

	if candidate.GradeKey == "" {
		return candidate, nil
	}
	ingredient, err := s.loadIngredient(ctx, candidate.GradeKey)
	if err != nil {
		return nil, err
	}
	candidate.Ingredient = *ingredient
	return candidate, nil
}

func (s *Store) nextCandidate() *Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return &Candidate{
		GradeKey: nextKey,
		Reviewed: reviewedCount,
		Total:    total,
	}
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

	prefix, err := s.prefix()
	if err != nil {
		return fmt.Errorf("generate ingredient grade prefix: %w", err)
	}
	gradeKeys, err := s.cache.List(ctx, grading.CachePrefix()+prefix, "")
	if err != nil {
		return fmt.Errorf("list ingredient grades: %w", err)
	}
	reviewKeys, err := s.cache.List(ctx, reviewCachePrefix+prefix, "")
	if err != nil {
		return fmt.Errorf("list ingredient grade reviews: %w", err)
	}

	gradeKeySet := make(map[string]struct{}, len(gradeKeys))
	for i, key := range gradeKeys {
		key = prefix + key
		gradeKeys[i] = key
		gradeKeySet[key] = struct{}{}
	}
	reviewed := make(map[string]struct{}, len(reviewKeys))
	for _, key := range reviewKeys {
		key = prefix + key
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
	if len(s.reviewed) == len(s.gradeKeys) {
		s.loaded = false
	}
	s.mu.Unlock()
}

func randomPrefix() (string, error) {
	var bytes [2]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return string([]byte{
		prefixAlphabet[int(bytes[0])%len(prefixAlphabet)],
		prefixAlphabet[int(bytes[1])%len(prefixAlphabet)],
	}), nil
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
