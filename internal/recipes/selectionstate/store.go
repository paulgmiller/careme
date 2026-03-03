package selectionstate

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
)

const cachePrefix = "recipe_selection/"

// State tracks which recipes have been saved and dismissed by a user
// between regeneration/finalization.
type State struct {
	SavedHashes     []string  `json:"saved_hashes,omitempty"`
	DismissedHashes []string  `json:"dismissed_hashes,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

func (s *State) MarkSaved(recipeHash string) {
	hash := strings.TrimSpace(recipeHash)
	if hash == "" {
		return
	}
	s.SavedHashes = lo.Uniq(append(s.SavedHashes, hash))
	s.DismissedHashes = lo.Filter(s.DismissedHashes, func(v string, _ int) bool { return v != hash })
}

func (s State) Empty() bool {
	return len(s.SavedHashes) == 0 && len(s.DismissedHashes) == 0
}

func (s *State) MarkDismissed(recipeHash string) {
	hash := strings.TrimSpace(recipeHash)
	if hash == "" {
		return
	}
	s.DismissedHashes = lo.Uniq(append(s.DismissedHashes, hash))
	s.SavedHashes = lo.Filter(s.SavedHashes, func(v string, _ int) bool { return v != hash })
}

func cacheKey(userID, originHash string) string {
	return fmt.Sprintf("%s%s/%s", cachePrefix, strings.TrimSpace(userID), strings.TrimSpace(originHash))
}

type Store struct {
	cache cache.Cache
}

func NewStore(c cache.Cache) *Store {
	return &Store{cache: c}
}

func (s *Store) Load(ctx context.Context, userID, originHash string) (State, error) {
	reader, err := s.cache.Get(ctx, cacheKey(userID, originHash))
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return State{}, nil
		}
		return State{}, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var state State
	if err := json.NewDecoder(reader).Decode(&state); err != nil {
		return State{}, fmt.Errorf("failed to decode recipe selection: %w", err)
	}
	return state, nil
}

func (s *Store) Save(ctx context.Context, userID, originHash string, state State) error {
	state.UpdatedAt = time.Now()
	body, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal recipe selection: %w", err)
	}
	if err := s.cache.Put(ctx, cacheKey(userID, originHash), string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("failed to save recipe selection: %w", err)
	}
	return nil
}

func RecipesForHashes(ctx context.Context, hashes []string, current []ai.Recipe, loadByHash func(context.Context, string) (*ai.Recipe, error)) []ai.Recipe {
	if len(hashes) == 0 {
		return nil
	}
	currentByHash := make(map[string]ai.Recipe, len(current))
	for _, recipe := range current {
		currentByHash[recipe.ComputeHash()] = recipe
	}

	recipes := make([]ai.Recipe, 0, len(hashes))
	for _, hash := range hashes {
		if recipe, ok := currentByHash[hash]; ok {
			recipes = append(recipes, recipe)
			continue
		}
		recipe, err := loadByHash(ctx, hash)
		if err != nil {
			continue
		}
		recipes = append(recipes, *recipe)
	}
	return recipes
}
