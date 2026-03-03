package recipes

import (
	"careme/internal/ai"
	"careme/internal/recipes/generation"
	"careme/internal/seasons"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
)

func (s *server) kickgeneration(ctx context.Context, p *generatorParams, currentUser *utypes.User) {
	hash := p.Hash()
	lastRecipes := make([]generation.LastRecipe, 0, len(currentUser.LastRecipes))
	for _, last := range currentUser.LastRecipes {
		lastRecipes = append(lastRecipes, generation.LastRecipe{
			Title:     last.Title,
			CreatedAt: last.CreatedAt,
		})
	}

	s.generationRunner().Kick(generation.Task{
		Ctx:         ctx,
		Hash:        hash,
		Params:      p.String(),
		LastRecipes: lastRecipes,
		AddLastRecipe: func(title string) {
			p.LastRecipes = append(p.LastRecipes, title)
		},
		Generate: func(ctx context.Context) (any, error) {
			return s.generator.GenerateRecipes(ctx, p)
		},
		Save: func(ctx context.Context, result any) error {
			shoppingList, ok := result.(*ai.ShoppingList)
			if !ok {
				return fmt.Errorf("unexpected generation result type %T", result)
			}
			return s.SaveShoppingList(ctx, shoppingList, hash)
		},
	})
}

func (s *server) Spin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	ctx := r.Context()
	spinnerData := struct {
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		RefreshInterval string // seconds
	}{
		ClarityScript:   templates.ClarityScript(),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
		RefreshInterval: "10", // seconds
	}

	if err := templates.Spin.Execute(w, spinnerData); err != nil {
		slog.ErrorContext(ctx, "home template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *server) Wait() {
	s.generationRunner().Wait()
}

func (s *server) generationRunner() *generation.Runner {
	if s.genRunner == nil {
		s.genRunner = generation.NewRunner()
	}
	return s.genRunner
}
