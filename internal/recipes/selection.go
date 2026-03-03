package recipes

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

const recipeSelectionCachePrefix = "recipe_selection/"

type recipeSelection struct {
	SavedHashes     []string  `json:"saved_hashes,omitempty"`
	DismissedHashes []string  `json:"dismissed_hashes,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

func (s *recipeSelection) markSaved(recipeHash string) {
	hash := strings.TrimSpace(recipeHash)
	if hash == "" {
		return
	}
	s.SavedHashes = lo.Uniq(append(s.SavedHashes, hash))
	s.DismissedHashes = lo.Filter(s.DismissedHashes, func(v string, _ int) bool { return v != hash })
}

func (s *recipeSelection) Empty() bool {
	return len(s.SavedHashes) == 0 && len(s.DismissedHashes) == 0
}

func (s *recipeSelection) markDismissed(recipeHash string) {
	hash := strings.TrimSpace(recipeHash)
	if hash == "" {
		return
	}
	s.DismissedHashes = lo.Uniq(append(s.DismissedHashes, hash))
	s.SavedHashes = lo.Filter(s.SavedHashes, func(v string, _ int) bool { return v != hash })
}

func recipeSelectionKey(userID, originHash string) string {
	return fmt.Sprintf("%s%s/%s", recipeSelectionCachePrefix, strings.TrimSpace(userID), strings.TrimSpace(originHash))
}

// this should die off eventually.
func recipeSelectionFromParams(p *generatorParams) recipeSelection {
	if p == nil {
		return recipeSelection{}
	}
	selection := recipeSelection{
		SavedHashes:     make([]string, 0, len(p.Saved)),
		DismissedHashes: make([]string, 0, len(p.Dismissed)),
	}
	for _, r := range p.Saved {
		selection.SavedHashes = append(selection.SavedHashes, r.ComputeHash())
	}
	for _, r := range p.Dismissed {
		selection.DismissedHashes = append(selection.DismissedHashes, r.ComputeHash())
	}
	return selection
}

func (s *server) loadRecipeSelection(ctx context.Context, userID, originHash string) (recipeSelection, error) {
	reader, err := s.Cache.Get(ctx, recipeSelectionKey(userID, originHash))
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return recipeSelection{}, nil
		}
		return recipeSelection{}, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var selection recipeSelection
	if err := json.NewDecoder(reader).Decode(&selection); err != nil {
		return recipeSelection{}, fmt.Errorf("failed to decode recipe selection: %w", err)
	}
	return selection, nil
}

func (s *server) saveRecipeSelection(ctx context.Context, userID, originHash string, selection recipeSelection) error {
	selection.UpdatedAt = time.Now()
	body, err := json.Marshal(selection)
	if err != nil {
		return fmt.Errorf("failed to marshal recipe selection: %w", err)
	}
	//good place for etags :)
	if err := s.Cache.Put(ctx, recipeSelectionKey(userID, originHash), string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("failed to save recipe selection: %w", err)
	}
	return nil
}

func (s *server) selectionRecipes(ctx context.Context, hashes []string, current []ai.Recipe) []ai.Recipe {
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
		recipe, err := s.SingleFromCache(ctx, hash)
		if err != nil {
			continue
		}
		recipes = append(recipes, *recipe)
	}
	return recipes
}

func (s *server) mergeParamsWithSelection(ctx context.Context, p *generatorParams, selection recipeSelection, current []ai.Recipe) {
	if p == nil {
		return
	}

	merged := recipeSelectionFromParams(p)
	for _, hash := range selection.SavedHashes {
		merged.markSaved(hash)
	}
	for _, hash := range selection.DismissedHashes {
		merged.markDismissed(hash)
	}

	p.Saved = s.selectionRecipes(ctx, merged.SavedHashes, current)
	p.Dismissed = s.selectionRecipes(ctx, merged.DismissedHashes, current)
}

func applySavedToRecipes(recipes []ai.Recipe, p *generatorParams) {
	saved := make(map[string]struct{}, len(p.Saved))
	for _, r := range p.Saved {
		saved[r.ComputeHash()] = struct{}{}
	}
	for i := range recipes {
		hash := recipes[i].ComputeHash()
		_, recipes[i].Saved = saved[hash]
	}
}
