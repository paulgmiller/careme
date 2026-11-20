package recipes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mealHTTPServer(t *testing.T, c cache.ListCache, g generator) (*server, *http.ServeMux) {
	t.Helper()
	options := []testServerOption{withTestCache(c), withTestClerk(noSessionAuth{})}
	if g != nil {
		options = append(options, withTestGenerator(g))
	}
	s := newTestServer(t, options...)
	mux := http.NewServeMux()
	s.registerShoppingListRoutes(mux)
	return s, mux
}

func mealPost(mux http.Handler, path, revision string, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"revision": {revision}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestMealReplacementHTTP(t *testing.T) {
	t.Parallel()
	c := cache.NewInMemoryCache()
	owner, _ := mealHTTPServer(t, c, nil)
	replica, mux := mealHTTPServer(t, c, nil)
	p := DefaultParams(&locations.Location{ID: "70000002", Name: "Test Store"}, time.Now())
	require.NoError(t, owner.SaveParams(t.Context(), p))
	hash := p.Hash()
	progress := owner.generationStatuses.(*status.Store)
	require.NoError(t, progress.Start(t.Context(), hash, status.InitialMessage))
	require.NoError(t, progress.Plan(t.Context(), hash, []ai.RecipePlan{{Cuisine: "Pending idea"}, {Cuisine: "Draft idea"}}))
	draft := ai.Recipe{Title: "Old draft details", OriginHash: hash}
	require.NoError(t, owner.SaveRecipe(t.Context(), draft))
	require.NoError(t, progress.RecipeDraft(t.Context(), hash, 1, draft.ComputeHash()))
	path := "/recipes/" + hash + "/slots/1/replace"
	rr := mealPost(mux, path, "0", true)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	body := rr.Body.String()
	assert.Contains(t, body, `id="shopping-content"`)
	assert.NotContains(t, body, "<!doctype html>")
	assert.NotContains(t, body, draft.Title)
	assert.NotContains(t, body, `shopping-recipe-`+strings.TrimRight(draft.ComputeHash(), "=")+`-details`)
	assert.Contains(t, body, "Finding another meal")
	assert.NotContains(t, body, `hx-post="`+path+`"`)
	assert.Contains(t, body, `/slots/0/replace`)
	got, err := replica.generationStatuses.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.True(t, got.Slots[1].Replacing)
	assert.Empty(t, got.Slots[1].RecipeHash)

	require.NoError(t, progress.ReplaceSlot(t.Context(), hash, 1, 1, ai.RecipePlan{Cuisine: "New meal idea"}))
	// A stale form refreshes to the new actionable revision without replacing it.
	stale := mealPost(mux, path, "0", true)
	require.Equal(t, http.StatusOK, stale.Code, stale.Body.String())
	assert.Contains(t, stale.Body.String(), "New meal idea")
	assert.Contains(t, stale.Body.String(), `name="revision" value="1"`)
	assert.Contains(t, stale.Body.String(), `hx-post="`+path+`"`)
	got, err = progress.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.False(t, got.Slots[1].Replacing)
	// The new revision accepts replacement from an ordinary browser form.
	ordinary := mealPost(mux, path, "1", false)
	require.Equal(t, http.StatusSeeOther, ordinary.Code)
	redirect, err := url.Parse(ordinary.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, hash, redirect.Query().Get("h"))
	require.NoError(t, progress.ReplaceSlot(t.Context(), hash, 1, 2, ai.RecipePlan{Cuisine: "Final idea"}))
	ready := ai.Recipe{Title: "Reviewed final meal", OriginHash: hash}
	require.NoError(t, owner.SaveRecipe(t.Context(), ready))
	won, err := progress.FinishSlot(t.Context(), hash, 1, 2, ready.ComputeHash())
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, progress.RecipeReady(t.Context(), hash, 1, ready.ComputeHash()))
	reviewed := mealPost(mux, path, "2", true)
	require.Equal(t, http.StatusOK, reviewed.Code, reviewed.Body.String())
	assert.Contains(t, reviewed.Body.String(), ready.Title)
	assert.Contains(t, reviewed.Body.String(), `/recipe/`+ready.ComputeHash()+`/save`)
	assert.NotContains(t, reviewed.Body.String(), `hx-post="`+path+`"`)
}

func TestMealReplacementHTTPMalformed(t *testing.T) {
	t.Parallel()
	_, mux := mealHTTPServer(t, cache.NewInMemoryCache(), nil)
	for _, tc := range []struct{ index, revision string }{{"oops", "0"}, {"-1", "0"}, {"0", ""}, {"0", "oops"}, {"0", "-1"}} {
		rr := mealPost(mux, "/recipes/hash/slots/"+tc.index+"/replace", tc.revision, true)
		assert.Equal(t, http.StatusBadRequest, rr.Code, "%+v", tc)
	}
}

type mealRetryGenerator struct {
	captureKickgenerationGenerator
	count   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (g *mealRetryGenerator) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	g.count.Add(1)
	g.started <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-g.release:
	}
	return g.captureKickgenerationGenerator.GenerateRecipes(ctx, p)
}

func TestMealRetryConcurrentReplicas(t *testing.T) {
	t.Parallel()
	c := cache.NewInMemoryCache()
	g := &mealRetryGenerator{started: make(chan struct{}, 2), release: make(chan struct{}), captureKickgenerationGenerator: captureKickgenerationGenerator{shoppingList: &ai.ShoppingList{Recipes: []ai.Recipe{{Title: "Retry meal"}}}}}
	owner, ownerMux := mealHTTPServer(t, c, g)
	replica, replicaMux := mealHTTPServer(t, c, g)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(g.release) }) }
	t.Cleanup(func() { release(); owner.wg.Wait(); replica.wg.Wait() })
	p := DefaultParams(&locations.Location{ID: "70000002", Name: "Test Store"}, time.Now())
	require.NoError(t, owner.SaveParams(t.Context(), p))
	hash := p.Hash()
	progress := owner.generationStatuses.(*status.Store)
	require.NoError(t, progress.Start(t.Context(), hash, "original run"))
	require.NoError(t, progress.Fail(t.Context(), hash, errors.New("original failed")))
	retry := *p
	retry.RetryOf = hash
	retryHash := retry.Hash()
	require.NotEqual(t, hash, retryHash)
	replies := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i, mux := range []http.Handler{ownerMux, replicaMux} {
		wg.Go(func() { replies[i] = mealPost(mux, "/recipes/"+hash+"/retry", "", false) })
	}
	wg.Wait()
	for _, rr := range replies {
		require.Equal(t, http.StatusSeeOther, rr.Code, rr.Body.String())
		redirect, err := url.Parse(rr.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, retryHash, redirect.Query().Get("h"))
	}
	select {
	case <-g.started:
	case <-time.After(5 * time.Second):
		t.Fatal("retry generation never started")
	}
	assert.Equal(t, int32(1), g.count.Load())
	saved, err := replica.ParamsFromCache(t.Context(), retryHash)
	require.NoError(t, err)
	assert.Equal(t, hash, saved.RetryOf)
	assert.Equal(t, retryHash, saved.Hash())
	old, err := progress.Load(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "original failed", old.Failed)
	assert.Equal(t, "original run", old.Message)
	// A late old worker can only publish under its original URL.
	require.NoError(t, owner.SaveShoppingList(t.Context(), &ai.ShoppingList{Recipes: []ai.Recipe{{Title: "Late original meal"}}}, hash))
	release()
	owner.wg.Wait()
	replica.wg.Wait()
	current, err := replica.FromCache(t.Context(), retryHash)
	require.NoError(t, err)
	require.Len(t, current.Recipes, 1)
	assert.Equal(t, "Retry meal", current.Recipes[0].Title)
	original, err := owner.FromCache(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, "Late original meal", original.Recipes[0].Title)
	assert.Equal(t, int32(1), g.count.Load())
}

func TestMealRetryActiveJoinsOriginal(t *testing.T) {
	t.Parallel()
	c := cache.NewInMemoryCache()
	g := &mealRetryGenerator{started: make(chan struct{}, 1), release: make(chan struct{})}
	s, mux := mealHTTPServer(t, c, g)
	p := DefaultParams(&locations.Location{ID: "70000002"}, time.Now())
	require.NoError(t, s.SaveParams(t.Context(), p))
	require.NoError(t, s.generationStatuses.Start(t.Context(), p.Hash(), "active"))
	rr := mealPost(mux, "/recipes/"+p.Hash()+"/retry", "", false)
	require.Equal(t, http.StatusSeeOther, rr.Code)
	target, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, p.Hash(), target.Query().Get("h"))
	s.wg.Wait()
	assert.Zero(t, g.count.Load())
}
