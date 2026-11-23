package recipes

import (
	"context"
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
	"careme/internal/recipes/feedback"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAugmentParamsRecipeCooldowns(t *testing.T) {
	t.Parallel()
	s := newTestServer(t)
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	user := utypes.User{ID: "user", Directive: "Vegetarian"}
	for _, tc := range []struct {
		title  string
		age    time.Duration
		cooked bool
	}{
		{"Saved recently", 7*24*time.Hour - time.Nanosecond, false},
		{"Saved boundary", 8 * 24 * time.Hour, false},
		{"Saved old", 9 * 24 * time.Hour, false},
		{"Cooked recently", 13 * 24 * time.Hour, true},
		{"Cooked boundary", 14 * 24 * time.Hour, true},
		{"Cooked old", 15 * 24 * time.Hour, true},
	} {
		r := utypes.Recipe{Title: tc.title, Hash: tc.title, CreatedAt: now.Add(-tc.age)}
		user.LastRecipes = append(user.LastRecipes, r)
		if tc.cooked {
			require.NoError(t, s.SaveFeedback(t.Context(), r.Hash, feedback.Feedback{Cooked: true, UpdatedAt: now}))
		}
	}
	user.DismissedRecipes = []utypes.Recipe{
		{Title: "Dismissed recently", Hash: "dismissed", CreatedAt: now},
		{Title: "Dismissed boundary", Hash: "boundary", CreatedAt: now.Add(-8 * 24 * time.Hour)},
		{Title: "Dismissed old", Hash: "old", CreatedAt: now.Add(-8 * 24 * time.Hour)},
	}
	p := &generatorParams{}
	augmentParamsFromUserAt(t.Context(), user, s.FeedbackIO, p, now)
	assert.ElementsMatch(t, []string{"Saved recently", "Cooked recently", "Dismissed recently"}, p.LastRecipes)
	assert.Equal(t, user.Directive, p.Directive)
}

type failedDecisionCache struct{ cache.ListCache }

func (c failedDecisionCache) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	if strings.HasPrefix(key, "users/") {
		return errors.New("user storage unavailable")
	}
	return c.ListCache.Put(ctx, key, value, opts)
}

func TestRecipeActionsRecordDecisions(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "storage failure"}[fail], func(t *testing.T) {
			var c cache.ListCache = cache.NewInMemoryCache()
			s := newTestServer(t, withTestCache(c))
			_, err := s.storage.FromRequest(t.Context(), httptest.NewRequest(http.MethodGet, "/", nil), s.clerk)
			require.NoError(t, err)
			if fail {
				s = newTestServer(t, withTestCache(failedDecisionCache{c}))
			}
			p := DefaultParams(&locations.Location{ID: "70000123"}, time.Now())
			require.NoError(t, s.SaveParams(t.Context(), p))
			recipe := ai.Recipe{Title: "Pasta", Instructions: []string{"Cook."}, OriginHash: p.Hash()}
			require.NoError(t, s.SaveRecipe(t.Context(), recipe))
			for _, action := range []string{"save", "dismiss", "save", "dismiss"} {
				req := httptest.NewRequest(http.MethodPost, "/recipe/"+recipe.ComputeHash()+"/"+action, strings.NewReader(url.Values{"h": {p.Hash()}}.Encode()))
				req.SetPathValue("hash", recipe.ComputeHash())
				req.Header.Set("HX-Request", "true")
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				rr := httptest.NewRecorder()
				if action == "save" {
					s.handleSaveRecipe(rr, req)
				} else {
					s.handleDismissRecipe(rr, req)
				}
				if fail {
					assert.Equal(t, http.StatusInternalServerError, rr.Code)
					continue
				}
				require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
				user, err := s.storage.GetByID("mock-clerk-user-id")
				require.NoError(t, err)
				if action == "save" {
					assert.Len(t, user.LastRecipes, 1)
				} else {
					assert.Empty(t, user.LastRecipes)
				}
				if action == "save" {
					assert.Empty(t, user.DismissedRecipes)
				} else {
					require.Len(t, user.DismissedRecipes, 1)
					assert.Equal(t, recipe.Title, user.DismissedRecipes[0].Title)
				}
				params := &generatorParams{}
				AugmentParamsFromUser(t.Context(), *user, s.FeedbackIO, params)
				assert.Equal(t, []string{recipe.Title}, params.LastRecipes)
			}
		})
	}
}
