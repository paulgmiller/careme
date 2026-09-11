package recipes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type progressAIClient struct {
	*captureRegenerateAIClient
	generate func(context.Context, []string) (*ai.Recipe, error)
}

func (c progressAIClient) GenerateRecipe(ctx context.Context, instructions []string, _ ai.ResponseRef) (*ai.Recipe, error) {
	return c.generate(ctx, instructions)
}

type notifyingProgress struct {
	*status.Store
	ready chan int
}

func (p notifyingProgress) RecipeReady(ctx context.Context, hash string, index int, recipeHash string) error {
	if err := p.Store.RecipeReady(ctx, hash, index, recipeHash); err != nil {
		return err
	}
	p.ready <- index
	return nil
}

func TestGenerationPublishesFinalRecipesInPlanOrder(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		name := "initial"
		if replacement {
			name = "replacement"
		}
		t.Run(name, func(t *testing.T) {
			c := cache.NewInMemoryCache()
			rio := IO(c)
			p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
			if replacement {
				p.PreviousMenuPlanResponseID = "previous-menu"
			}
			progress := notifyingProgress{Store: status.NewStore(c), ready: make(chan int, 2)}
			require.NoError(t, progress.Start(t.Context(), p.Hash()))
			releaseReview := make(chan struct{})
			reviewStarted := make(chan struct{})
			defer close(releaseReview)
			client := progressAIClient{
				captureRegenerateAIClient: &captureRegenerateAIClient{menuPlan: &ai.MenuPlan{
					ResponseID: "menu", Plans: []ai.RecipePlan{{Cuisine: "Slow"}, {Cuisine: "Fast"}},
				}},
				generate: func(_ context.Context, instructions []string) (*ai.Recipe, error) {
					title := "Fast"
					if strings.Contains(strings.Join(instructions, " "), "Slow") {
						title = "Slow"
					}
					return &ai.Recipe{Title: title, ResponseID: "response-" + title}, nil
				},
			}
			critiquer := &captureCritiqueService{fn: func(recipe ai.Recipe) (*ai.RecipeCritique, error) {
				if recipe.Title == "Slow" {
					close(reviewStarted)
					<-releaseReview
				}
				return &ai.RecipeCritique{OverallScore: 10}, nil
			}}
			generator := newTestGenerator(t, client, critiquer, fixedStaplesService{}, progress, rio)
			done := make(chan error, 1)
			go func() { _, err := generator.GenerateRecipes(t.Context(), p); done <- err }()
			select {
			case <-reviewStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("review did not start")
			}
			select {
			case index := <-progress.ready:
				assert.Equal(t, 1, index)
			case <-time.After(5 * time.Second):
				t.Fatal("ready recipe not published")
			}
			got, err := progress.Load(t.Context(), p.Hash())
			require.NoError(t, err)
			require.Len(t, got.Slots, 2)
			assert.Empty(t, got.Slots[0].RecipeHash, "draft must stay hidden during review")
			assert.Equal(t, "Slow", got.Slots[0].Plan.Cuisine)
			recipe, err := rio.SingleFromCache(t.Context(), got.Slots[1].RecipeHash)
			require.NoError(t, err)
			assert.Equal(t, "Fast", recipe.Title)
			// Release in a defer so failed assertions also unblock the worker.
			t.Cleanup(func() { require.NoError(t, <-done) })
		})
	}
}

type failingProgress struct {
	noopstatuswriter
	failPlan bool
}

func (p failingProgress) Plan(context.Context, string, []ai.RecipePlan) error {
	if p.failPlan {
		return errors.New("progress storage unavailable")
	}
	return nil
}

func (p failingProgress) RecipeReady(context.Context, string, int, string) error {
	return errors.New("progress storage unavailable")
}

func TestGenerationFailsWhenStructuredProgressCannotBeStored(t *testing.T) {
	for _, failPlan := range []bool{true, false} {
		t.Run(map[bool]string{true: "plan", false: "ready recipe"}[failPlan], func(t *testing.T) {
			p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
			client := &captureGenerateAIClient{shoppingList: &ai.ShoppingList{Recipes: []ai.Recipe{{Title: "Dinner"}}}}
			generator := newTestGenerator(t, client, &captureCritiqueService{}, fixedStaplesService{}, failingProgress{failPlan: failPlan}, noopRecipeSaver{})
			list, err := generator.GenerateRecipes(t.Context(), p)
			require.ErrorContains(t, err, "progress storage unavailable")
			assert.Nil(t, list)
		})
	}
}
