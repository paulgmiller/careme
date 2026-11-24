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
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/templates"
	"careme/internal/users"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureLeftoversAI struct {
	MockLeftoversAI
	inputs  []ai.LeftoversInput
	err     error
	results []*ai.LeftoversResult
}

func (c *captureLeftoversAI) GenerateLeftoversMeal(ctx context.Context, input ai.LeftoversInput) (*ai.LeftoversResult, error) {
	c.inputs = append(c.inputs, input)
	if c.err != nil {
		return nil, c.err
	}
	if len(c.results) > 0 {
		result := c.results[0]
		c.results = c.results[1:]
		return result, nil
	}
	return c.MockLeftoversAI.GenerateLeftoversMeal(ctx, input)
}

func leftoversTestHandler(t *testing.T) (*leftoversHandler, *captureLeftoversAI, *utypes.User, *http.ServeMux) {
	t.Helper()
	require.NoError(t, templates.Init(&config.Config{}))
	c := cache.NewInMemoryCache()
	storage := users.NewStorage(c)
	clerk := auth.DefaultMock()
	user, err := storage.FromRequest(t.Context(), httptest.NewRequest("GET", "/leftovers", nil), clerk)
	require.NoError(t, err)
	client := &captureLeftoversAI{}
	h := NewLeftoversHandler(client, c, storage, clerk, NewImageStore(c), NewMockImageGen())
	t.Cleanup(h.Wait)
	mux := http.NewServeMux()
	h.Register(mux)
	return h, client, user, mux
}

func postLeftovers(mux *http.ServeMux, path string, values url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestLeftoversFlow(t *testing.T) {
	h, client, user, mux := leftoversTestHandler(t)
	user.Directive = "make it vegetarian"
	user.LastRecipes = []utypes.Recipe{{Title: "Recent rice bowl", CreatedAt: time.Now()}, {Title: "Old soup", CreatedAt: time.Now().AddDate(0, 0, -20)}}
	require.NoError(t, h.users.Update(user))
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest("GET", "/leftovers", nil))
	assert.Equal(t, http.StatusOK, get.Code)
	assert.Contains(t, get.Body.String(), `name="leftovers"`)
	assert.Contains(t, get.Body.String(), "leftovers.js")
	first := postLeftovers(mux, "/leftovers", url.Values{"leftovers": {"cooked rice"}, "meal": {"lunch"}, "servings": {"3"}})
	require.Equal(t, http.StatusSeeOther, first.Code, first.Body.String())
	first = getLeftovers(mux, first.Header().Get("Location"))
	assert.Contains(t, first.Body.String(), "Do you have these on hand?")
	assert.Contains(t, first.Body.String(), `value="yes"`)
	assert.Contains(t, first.Body.String(), `value="no"`)
	keys, err := h.usersCacheKeys(t.Context())
	require.NoError(t, err)
	require.Len(t, keys, 1)
	token := strings.TrimPrefix(keys[0], "leftovers/")
	answers := url.Values{"round": {"0"}, "ingredient-0": {"yes"}, "ingredient-1": {"no"}, "ingredient-2": {"yes"}, "ingredient-3": {"no"}, "leftovers": {"tampered"}}
	result := postLeftovers(mux, "/leftovers/"+token, answers)
	require.Equal(t, http.StatusSeeOther, result.Code, result.Body.String())
	require.Len(t, client.inputs, 1)
	input := client.inputs[0]
	assert.Equal(t, "cooked rice", input.Leftovers)
	assert.Equal(t, "lunch", input.Meal)
	assert.Equal(t, 3, input.Servings)
	assert.Equal(t, "make it vegetarian", input.Directive)
	assert.Equal(t, []string{"eggs", "spinach"}, input.Available)
	assert.Equal(t, []string{"bread", "olive oil"}, input.Unavailable)
	assert.Equal(t, []string{"Recent rice bowl"}, input.RecentMeals)
	result = getLeftovers(mux, result.Header().Get("Location"))
	assert.Equal(t, http.StatusSeeOther, result.Code)
	assert.Contains(t, result.Header().Get("Location"), "/recipe/")
	assert.NotContains(t, result.Body.String(), "Add to cart")
	updated, err := h.users.GetByID(user.ID)
	require.NoError(t, err)
	require.Len(t, updated.LastRecipes, 2, "generation must not save the recipe")
	again := postLeftovers(mux, "/leftovers/"+token, answers)
	assert.Equal(t, http.StatusSeeOther, again.Code)
	assert.Len(t, client.inputs, 1, "repeat submission should reuse the completed recipe")
	next := ai.LeftoversInput{}
	h.recentMeals(updated, &next)
	assert.NotContains(t, next.RecentMeals, "Leftovers for lunch")
}

func (h *leftoversHandler) usersCacheKeys(ctx context.Context) ([]string, error) {
	return h.io.Cache.(cache.ListCache).List(ctx, "leftovers/", "")
}

func TestLeftoversRequiresStartingFoodAndMeal(t *testing.T) {
	_, client, _, mux := leftoversTestHandler(t)
	for _, values := range []url.Values{
		{"leftovers": {"   "}, "meal": {"dinner"}},
		{"leftovers": {strings.Repeat("a", 2001)}, "meal": {"dinner"}},
		{"leftovers": {"rice"}, "meal": {"snack"}},
	} {
		w := postLeftovers(mux, "/leftovers", values)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
	assert.Empty(t, client.inputs)
}

func TestLeftoversRequiresAllAnswersAndOwnUnexpiredSession(t *testing.T) {
	h, client, user, mux := leftoversTestHandler(t)
	session := leftoversSession{Owner: user.ID, Expires: time.Now().Add(time.Hour), Input: ai.LeftoversInput{Leftovers: "rice", Meal: "dinner"}, Questions: []string{"eggs", "bread", "oil"}}
	require.NoError(t, h.saveSession(t.Context(), "test", session))
	for _, answers := range []url.Values{
		{"round": {"0"}},
		{"round": {"0"}, "ingredient-0": {"yes"}, "ingredient-1": {"no"}, "ingredient-2": {"maybe"}},
		{"round": {"0"}, "ingredient-0": {"yes", "no"}, "ingredient-1": {"no"}, "ingredient-2": {"no"}},
	} {
		assert.Equal(t, http.StatusBadRequest, postLeftovers(mux, "/leftovers/test", answers).Code)
	}
	session.Owner = "someone-else"
	require.NoError(t, h.saveSession(t.Context(), "test", session))
	assert.Equal(t, http.StatusNotFound, postLeftovers(mux, "/leftovers/test", nil).Code)
	session.Owner = user.ID
	session.Expires = time.Now().Add(-time.Minute)
	require.NoError(t, h.saveSession(t.Context(), "test", session))
	assert.Equal(t, http.StatusGone, postLeftovers(mux, "/leftovers/test", nil).Code)
	assert.Empty(t, client.inputs)
}

func TestLeftoversAllNoAndGenerationFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "all no", true: "provider failure"}[fail], func(t *testing.T) {
			h, client, user, mux := leftoversTestHandler(t)
			if fail {
				client.err = errors.New("provider unavailable")
			}
			session := leftoversSession{Owner: user.ID, Expires: time.Now().Add(time.Hour), Input: ai.LeftoversInput{Leftovers: "rice", Meal: "breakfast", Servings: 2}, Questions: []string{"eggs", "bread", "oil"}}
			require.NoError(t, h.saveSession(t.Context(), "test", session))
			w := postLeftovers(mux, "/leftovers/test", url.Values{"round": {"0"}, "ingredient-0": {"no"}, "ingredient-1": {"no"}, "ingredient-2": {"no"}})
			if fail {
				assert.Equal(t, http.StatusInternalServerError, w.Code)
				assert.Contains(t, w.Body.String(), "Try again, chef")
			} else {
				assert.Equal(t, http.StatusSeeOther, w.Code)
			}
			require.Len(t, client.inputs, 1)
			assert.Empty(t, client.inputs[0].Available)
			assert.Len(t, client.inputs[0].Unavailable, 3)
		})
	}
}

func getLeftovers(mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestLeftoversMoreQuestionsAndMakeNow(t *testing.T) {
	h, client, user, mux := leftoversTestHandler(t)
	session := leftoversSession{Owner: user.ID, Expires: time.Now().Add(time.Hour), Input: ai.LeftoversInput{Leftovers: "rice", Meal: "dinner", Servings: 2, Asked: []string{"eggs", "bread", "oil"}}, Questions: []string{"eggs", "bread", "oil"}}
	require.NoError(t, h.saveSession(t.Context(), "test", session))
	client.results = []*ai.LeftoversResult{{Questions: []string{"tomatoes", "onion"}}}
	first := postLeftovers(mux, "/leftovers/test", url.Values{"round": {"0"}, "ingredient-0": {"no"}, "ingredient-1": {"no"}, "ingredient-2": {"no"}})
	require.Equal(t, http.StatusSeeOther, first.Code)
	page := getLeftovers(mux, "/leftovers/test")
	assert.Contains(t, page.Body.String(), "tomatoes")
	assert.Contains(t, page.Body.String(), "Just make something")
	assert.NotContains(t, page.Body.String(), ">eggs</legend>")
	stale := postLeftovers(mux, "/leftovers/test", url.Values{"round": {"0"}})
	assert.Equal(t, http.StatusSeeOther, stale.Code)
	require.Len(t, client.inputs, 1, "stale round must not call AI or discard answers")
	second := postLeftovers(mux, "/leftovers/test", url.Values{"round": {"1"}, "action": {"now"}, "ingredient-0": {"yes"}})
	require.Equal(t, http.StatusSeeOther, second.Code)
	require.Len(t, client.inputs, 2)
	assert.True(t, client.inputs[1].MakeNow)
	assert.Equal(t, []string{"tomatoes"}, client.inputs[1].Available)
	assert.Equal(t, []string{"eggs", "bread", "oil", "onion"}, client.inputs[1].Unavailable)
	assert.Len(t, client.inputs[1].Asked, 5)
}

func TestLeftoversHonestExplanationAllowsRevision(t *testing.T) {
	h, client, user, mux := leftoversTestHandler(t)
	session := leftoversSession{Owner: user.ID, Expires: time.Now().Add(time.Hour), Input: ai.LeftoversInput{Leftovers: "cake frosting", Meal: "dinner", Servings: 2, Asked: []string{"eggs", "bread", "oil"}}, Questions: []string{"eggs", "bread", "oil"}}
	require.NoError(t, h.saveSession(t.Context(), "test", session))
	client.results = []*ai.LeftoversResult{{Explanation: "Frosting alone will not make a cohesive dinner."}}
	result := postLeftovers(mux, "/leftovers/test", url.Values{"round": {"0"}, "action": {"now"}})
	require.Equal(t, http.StatusSeeOther, result.Code)
	page := getLeftovers(mux, "/leftovers/test")
	assert.Contains(t, page.Body.String(), "Frosting alone")
	assert.Contains(t, page.Body.String(), `name="leftovers"`)
	assert.NotContains(t, page.Body.String(), "Just make something")
	updated, err := h.users.GetByID(user.ID)
	require.NoError(t, err)
	assert.Empty(t, updated.LastRecipes)
}

func TestLeftoversRecipeUsesExistingSaveAndFeedbackPage(t *testing.T) {
	h, _, user, _ := leftoversTestHandler(t)
	recipe := ai.Recipe{Leftovers: true, Title: "Crisp rice", Description: "A new use for rice", ResponseID: "resp-leftovers", Ingredients: []ai.Ingredient{{Name: "cooked rice", Quantity: "2 cups"}}, Instructions: []string{"Warm 2 cups cooked rice."}}
	require.NoError(t, h.io.SaveRecipe(t.Context(), recipe))
	s := newTestServer(t, withTestCache(h.io.Cache.(cache.ListCache)), withTestStorage(h.users))
	mux := http.NewServeMux()
	s.Register(mux)
	hash := recipe.ComputeHash()
	page := getLeftovers(mux, "/recipe/"+hash)
	require.Equal(t, http.StatusOK, page.Code, page.Body.String())
	assert.Contains(t, page.Body.String(), `hx-post="/recipe/`+hash+`/save"`)
	assert.Contains(t, page.Body.String(), `action="/recipe/`+hash+`/question"`)
	assert.Contains(t, page.Body.String(), `action="/recipe/`+hash+`/feedback"`)
	assert.NotContains(t, page.Body.String(), "Back to shopping list")
	assert.NotContains(t, page.Body.String(), "Add to cart")
	assert.NotContains(t, page.Body.String(), "Dismiss")
	for _, action := range []string{"save", "save", "dismiss"} {
		request := httptest.NewRequest("POST", "/recipe/"+hash+"/"+action, strings.NewReader("source=recipe"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("HX-Request", "true")
		result := httptest.NewRecorder()
		mux.ServeHTTP(result, request)
		require.Equal(t, http.StatusOK, result.Code, result.Body.String())
		updated, err := h.users.GetByID(user.ID)
		require.NoError(t, err)
		var input ai.LeftoversInput
		h.recentMeals(updated, &input)
		if action == "save" {
			require.Len(t, updated.LastRecipes, 1)
			assert.Contains(t, input.RecentMeals, recipe.Title)
			assert.Contains(t, result.Body.String(), "Dismiss")
		} else {
			assert.Empty(t, updated.LastRecipes)
			assert.Empty(t, input.RecentMeals)
		}
	}
}

func TestLeftoversRequiresSignIn(t *testing.T) {
	h, client, _, _ := leftoversTestHandler(t)
	h.auth = noSessionAuth{}
	mux := http.NewServeMux()
	h.Register(mux)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/leftovers"}, {"POST", "/leftovers"}, {"GET", "/leftovers/test"}, {"POST", "/leftovers/test"}} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		assert.Equal(t, http.StatusSeeOther, response.Code)
		assert.Contains(t, response.Header().Get("Location"), "/sign-in")
	}
	assert.Empty(t, client.inputs)
}

func TestLeftoversImageGeneratesAsynchronouslyBeforeSave(t *testing.T) {
	h, _, user, mux := leftoversTestHandler(t)
	imageCache := cache.NewInMemoryCache()
	imageStore := NewImageStore(imageCache)
	provider := blockingRecipeImageGenerator{started: make(chan struct{}), release: make(chan struct{})}
	h.images = PostRecipeJobs{NewRecipeImageGenerator(imageStore, provider)}.Background()
	defer func() { close(provider.release); h.Wait() }()
	session := leftoversSession{Owner: user.ID, Expires: time.Now().Add(time.Hour), Input: ai.LeftoversInput{Leftovers: "rice", Meal: "dinner", Servings: 2, Asked: []string{"eggs", "bread", "oil"}}, Questions: []string{"eggs", "bread", "oil"}}
	require.NoError(t, h.saveSession(t.Context(), "image", session))
	response := postLeftovers(mux, "/leftovers/image", url.Values{"round": {"0"}, "action": {"now"}})
	require.Equal(t, http.StatusSeeOther, response.Code, "the recipe response must not wait for the blocked image")
	select {
	case <-provider.started:
	case <-time.After(5 * time.Second):
		t.Fatal("image generation did not start")
	}
	location := getLeftovers(mux, response.Header().Get("Location")).Header().Get("Location")
	hash := strings.TrimPrefix(location, "/recipe/")
	exists, err := imageStore.Exists(t.Context(), hash)
	require.NoError(t, err)
	assert.False(t, exists, "recipe is usable before its image is ready")
	updated, err := h.users.GetByID(user.ID)
	require.NoError(t, err)
	assert.Empty(t, updated.LastRecipes, "generating an image must not save the recipe")
	// Re-submitting a completed session must reuse the existing recipe and image job.
	again := postLeftovers(mux, "/leftovers/image", url.Values{"round": {"0"}, "action": {"now"}})
	assert.Equal(t, http.StatusSeeOther, again.Code)
}

func TestLeftoversServingsRequiredAndValidated(t *testing.T) {
	_, client, _, mux := leftoversTestHandler(t)
	for _, servings := range []string{"", "0", "-1", "13", "two", "2.5"} {
		w := postLeftovers(mux, "/leftovers", url.Values{"leftovers": {"rice"}, "meal": {"dinner"}, "servings": {servings}})
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Choose between 1 and 12 servings")
	}
	assert.Empty(t, client.inputs)
	page := getLeftovers(mux, "/leftovers")
	assert.Contains(t, page.Body.String(), `name="servings"`)
	assert.Contains(t, page.Body.String(), `value="2"`)
}
