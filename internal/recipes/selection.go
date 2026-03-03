package recipes

import (
	"careme/internal/ai"
	"careme/internal/recipes/selectionstate"
	"context"
)

type recipeSelection = selectionstate.State

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

func (s *server) selectionRepo() *selectionstate.Store {
	if s.selectionStore == nil {
		s.selectionStore = selectionstate.NewStore(s.Cache)
	}
	return s.selectionStore
}

func (s *server) loadRecipeSelection(ctx context.Context, userID, originHash string) (recipeSelection, error) {
	return s.selectionRepo().Load(ctx, userID, originHash)
}

func (s *server) saveRecipeSelection(ctx context.Context, userID, originHash string, selection recipeSelection) error {
	return s.selectionRepo().Save(ctx, userID, originHash, selection)
}

func (s *server) selectionRecipes(ctx context.Context, hashes []string, current []ai.Recipe) []ai.Recipe {
	return selectionstate.RecipesForHashes(ctx, hashes, current, s.SingleFromCache)
}

func (s *server) mergeParamsWithSelection(ctx context.Context, p *generatorParams, selection recipeSelection, current []ai.Recipe) {
	if p == nil {
		return
	}

	merged := recipeSelectionFromParams(p)
	for _, hash := range selection.SavedHashes {
		merged.MarkSaved(hash)
	}
	for _, hash := range selection.DismissedHashes {
		merged.MarkDismissed(hash)
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
