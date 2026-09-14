package recipes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitialCritiqueFailureLeavesSlotDraft(t *testing.T) {
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
	c := cache.NewInMemoryCache()
	progress := status.NewStore(c)
	require.NoError(t, progress.Start(t.Context(), p.Hash(), ""))
	client := &sequenceAIClient{generateResponses: []*ai.ShoppingList{{Recipes: []ai.Recipe{{Title: "Draft", ResponseID: "response"}}}}}
	g := newTestGenerator(t, client, &captureCritiqueService{err: errors.New("review unavailable")}, seededStaples(t, p), progress, IO(c))
	result, err := g.GenerateRecipes(t.Context(), p)
	require.ErrorContains(t, err, "review unavailable")
	assert.Nil(t, result)
	state, err := progress.Load(t.Context(), p.Hash())
	require.NoError(t, err)
	require.Len(t, state.Slots, 1)
	assert.False(t, state.Slots[0].Reviewed)
}

func TestDraftCardHasDetailsButNoLinksOrAdd(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
	require.NoError(t, s.SaveParams(t.Context(), p))
	progress := s.generationStatuses.(*status.Store)
	require.NoError(t, progress.Start(t.Context(), p.Hash(), "Reviewing"))
	require.NoError(t, progress.Plan(t.Context(), p.Hash(), []ai.RecipePlan{{Cuisine: "Italian"}}))
	recipe := ai.Recipe{Title: "Dinner", Instructions: []string{"Cook the beans."}, OriginHash: p.Hash()}
	require.NoError(t, s.SaveRecipe(t.Context(), recipe))
	require.NoError(t, progress.RecipeDraft(t.Context(), p.Hash(), 0, recipe.ComputeHash()))
	poll := func() string {
		req := httptest.NewRequest(http.MethodGet, "/recipes?h="+p.Hash(), nil)
		req.Header.Set("HX-Request", "true")
		req.Header.Set("HX-Target", "shopping-content")
		page := httptest.NewRecorder()
		s.handleRecipes(page, req)
		require.Equal(t, http.StatusOK, page.Code)
		return page.Body.String()
	}
	body := poll()
	assert.Contains(t, body, recipe.Title)
	assert.Contains(t, body, "Cook the beans.")
	assert.Contains(t, body, "Details")
	assert.Contains(t, body, "Reviewing recipe…")
	assert.NotContains(t, body, `href="/recipe/`+recipe.ComputeHash())
	assert.NotContains(t, body, `hx-post="/recipe/`+recipe.ComputeHash()+`/save"`)
	require.NoError(t, progress.RecipeReady(t.Context(), p.Hash(), 0, recipe.ComputeHash()))
	body = poll()
	assert.Contains(t, body, `href="/recipe/`+recipe.ComputeHash())
	assert.Contains(t, body, `hx-post="/recipe/`+recipe.ComputeHash()+`/save"`)
	assert.NotContains(t, body, "Reviewing recipe…")
}

// Record background work without completing it to verify generation is ready
// while the second critique is still outstanding.
type deferredSecondCritique struct {
	queued []ai.Recipe
}

func (*deferredSecondCritique) CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error) {
	return &ai.RecipeCritique{OverallScore: 1}, nil
}

func (c *deferredSecondCritique) CritiqueRecipeInBackground(_ context.Context, recipe ai.Recipe) {
	c.queued = append(c.queued, recipe)
}

func TestRegeneratedSlotReadyBeforeBackgroundCritiqueCompletes(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial", true: "replacement"}[replacement], func(t *testing.T) {
			p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
			if replacement {
				p.PreviousMenuPlanResponseID = "previous-menu"
			}
			c := cache.NewInMemoryCache()
			progress := status.NewStore(c)
			require.NoError(t, progress.Start(t.Context(), p.Hash(), ""))
			revised := ai.Recipe{Title: "Revised dinner", ResponseID: "revision"}
			client := &sequenceAIClient{generateResponses: []*ai.ShoppingList{{Recipes: []ai.Recipe{{Title: "Draft", ResponseID: "response"}}}}, regenerateResponses: []*ai.Recipe{&revised}}
			critiquer := &deferredSecondCritique{}
			g := newTestGenerator(t, client, critiquer, seededStaples(t, p), progress, IO(c))
			result, err := g.GenerateRecipes(t.Context(), p)
			require.NoError(t, err)
			require.Len(t, result.Recipes, 1)
			require.Len(t, critiquer.queued, 1)
			state, err := progress.Load(t.Context(), p.Hash())
			require.NoError(t, err)
			require.Len(t, state.Slots, 1)
			assert.True(t, state.Slots[0].Reviewed)
			assert.Equal(t, revised.ComputeHash(), state.Slots[0].RecipeHash)
		})
	}
}
