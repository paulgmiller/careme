package recipes

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestCritiqueFailureNeverMarksSlotReviewed(t *testing.T) {
	for _, secondReview := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial critique error", true: "revision critique error"}[secondReview], func(t *testing.T) {
			p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
			c := cache.NewInMemoryCache()
			progress := status.NewStore(c)
			require.NoError(t, progress.Start(t.Context(), p.Hash(), ""))
			initial := ai.Recipe{Title: "Initial", ResponseID: "response"}
			revised := ai.Recipe{Title: "Revision", ResponseID: "revision-response"}
			critiquer := &captureCritiqueService{fn: func(recipe ai.Recipe) (*ai.RecipeCritique, error) {
				if secondReview && recipe.Title == initial.Title {
					return &ai.RecipeCritique{OverallScore: 1}, nil
				}
				return nil, errors.New("review unavailable")
			}}
			client := &sequenceAIClient{generateResponses: []*ai.ShoppingList{{Recipes: []ai.Recipe{initial}}}, regenerateResponses: []*ai.Recipe{&revised}}
			g := newTestGenerator(t, client, critiquer, seededStaples(t, p), progress, IO(c))
			result, err := g.GenerateRecipes(t.Context(), p)
			require.ErrorContains(t, err, "review unavailable")
			assert.Nil(t, result)
			state, err := progress.Load(t.Context(), p.Hash())
			require.NoError(t, err)
			require.Len(t, state.Slots, 1)
			assert.False(t, state.Slots[0].Reviewed)
		})
	}
}

func TestRecipeReviewGatesAddAndSave(t *testing.T) {
	for _, revised := range []bool{false, true} {
		t.Run(map[bool]string{false: "passes", true: "revised"}[revised], func(t *testing.T) {
			s := newTestServer(t)
			defer s.Wait()
			p := DefaultParams(&locations.Location{ID: "70000123", Name: "Store"}, time.Now())
			require.NoError(t, s.SaveParams(t.Context(), p))
			progress := s.generationStatuses.(*status.Store)
			require.NoError(t, progress.Start(t.Context(), p.Hash(), "Reviewing"))
			require.NoError(t, progress.Plan(t.Context(), p.Hash(), []ai.RecipePlan{{Cuisine: "Italian"}}))
			draft := ai.Recipe{Title: "Draft dinner", Instructions: []string{"Cook the beans."}, OriginHash: p.Hash()}
			require.NoError(t, s.SaveRecipe(t.Context(), draft))
			require.NoError(t, progress.RecipeDraft(t.Context(), p.Hash(), 0, draft.ComputeHash()))
			page := httptest.NewRecorder()
			s.handleRecipes(page, httptest.NewRequest(http.MethodGet, "/recipes?h="+p.Hash(), nil))
			require.Equal(t, http.StatusOK, page.Code)
			assert.Contains(t, page.Body.String(), draft.Title)
			assert.Contains(t, page.Body.String(), "Cook the beans.")
			assert.Contains(t, page.Body.String(), "Details")
			assert.Contains(t, page.Body.String(), "Reviewing recipe…")
			assert.NotContains(t, page.Body.String(), `hx-post="/recipe/`+draft.ComputeHash()+`/save"`)

			single := httptest.NewRequest(http.MethodGet, "/recipe/"+draft.ComputeHash(), nil)
			single.SetPathValue("hash", draft.ComputeHash())
			detail := httptest.NewRecorder()
			s.handleSingle(detail, single)
			require.Equal(t, http.StatusOK, detail.Code)
			assert.Contains(t, detail.Body.String(), "Cook the beans.")
			assert.Contains(t, detail.Body.String(), "Reviewing recipe…")
			assert.NotContains(t, detail.Body.String(), `hx-post="/recipe/`+draft.ComputeHash()+`/save"`)

			save := func(hash, list string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, "/recipe/"+hash+"/save", strings.NewReader(url.Values{"h": {list}}.Encode()))
				req.SetPathValue("hash", hash)
				req.Header.Set("HX-Request", "true")
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				response := httptest.NewRecorder()
				s.handleSaveRecipe(response, req)
				return response
			}
			assert.Equal(t, http.StatusConflict, save(draft.ComputeHash(), p.Hash()).Code)
			assert.Equal(t, http.StatusConflict, save(draft.ComputeHash(), "different-list").Code)
			selection, err := s.loadRecipeSelection(t.Context(), "mock-clerk-user-id", p.Hash())
			require.NoError(t, err)
			assert.Empty(t, selection.SavedHashes)

			final := draft
			if revised {
				final.Title = "Reviewed dinner"
				final.ParentHash = draft.ComputeHash()
				require.NoError(t, s.SaveRecipe(t.Context(), final))
				require.NoError(t, progress.RecipeDraft(t.Context(), p.Hash(), 0, final.ComputeHash()))
				assert.Equal(t, http.StatusConflict, save(final.ComputeHash(), p.Hash()).Code)
			}
			require.NoError(t, progress.RecipeReady(t.Context(), p.Hash(), 0, final.ComputeHash()))
			response := save(final.ComputeHash(), p.Hash())
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), "Recipe added")
			if revised {
				assert.Equal(t, http.StatusConflict, save(draft.ComputeHash(), p.Hash()).Code)
				require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{final}}, p.Hash()))
				assert.Equal(t, http.StatusConflict, save(draft.ComputeHash(), p.Hash()).Code)
			}
		})
	}
}
