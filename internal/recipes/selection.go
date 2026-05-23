package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"

	"github.com/samber/lo"
)

const recipeSelectionCachePrefix = "recipe_selection/"

// recipeSelection tracks which recipes have been saved and dismisse by a user between regeneration/finalization.
// After that they are merged back into params.
type recipeSelection struct {
	SavedHashes     []string  `json:"saved_hashes,omitempty"`
	DismissedHashes []string  `json:"dismissed_hashes,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
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

func (rio recipeio) loadRecipeSelection(ctx context.Context, userID, originHash string) (recipeSelection, error) {
	reader, err := rio.Cache.Get(ctx, recipeSelectionKey(userID, originHash))
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

func (rio recipeio) saveRecipeSelection(ctx context.Context, userID, originHash string, selection recipeSelection) error {
	selection.UpdatedAt = time.Now()
	body, err := json.Marshal(selection)
	if err != nil {
		return fmt.Errorf("failed to marshal recipe selection: %w", err)
	}
	// good place for etags :)
	if err := rio.Cache.Put(ctx, recipeSelectionKey(userID, originHash), string(body), cache.Unconditional()); err != nil {
		return fmt.Errorf("failed to save recipe selection: %w", err)
	}
	return nil
}

func selectionFromSaved(saved []ai.Recipe) recipeSelection {
	var selection recipeSelection
	for _, s := range saved {
		selection.markSaved(s.ComputeHash())
	}
	return selection
}

func (selection recipeSelection) override(new recipeSelection) recipeSelection {
	for _, hash := range new.SavedHashes {
		selection.markSaved(hash)
	}
	for _, hash := range new.DismissedHashes {
		selection.markDismissed(hash)
	}
	return selection
}

func (selection recipeSelection) IsSaved(hash string) bool {
	return slices.Contains(selection.SavedHashes, hash)
}

func (selection recipeSelection) IsDismissed(hash string) bool {
	return slices.Contains(selection.DismissedHashes, hash)
}
