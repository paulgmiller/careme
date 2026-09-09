package campaigns

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/recipes"
	"careme/internal/recipes/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type advertisedLocationStoreStub struct{}

func (advertisedLocationStoreStub) GetLocationByID(_ context.Context, id string) (*locations.Location, error) {
	lat, lon := 47.61, -122.33
	return &locations.Location{ID: id, Name: "Hydrated " + id, Address: id + " Market St", ZipCode: "98101", Lat: &lat, Lon: &lon}, nil
}

type campaignGeneratorStub struct {
	params   []*recipes.GeneratorParams
	contexts []context.Context
	err      error
}

func (g *campaignGeneratorStub) GenerateRecipes(ctx context.Context, p *recipes.GeneratorParams) (*ai.ShoppingList, error) {
	g.params = append(g.params, p)
	g.contexts = append(g.contexts, ctx)
	if g.err != nil {
		return nil, g.err
	}
	return &ai.ShoppingList{Recipes: []ai.Recipe{{Title: "Dinner at " + p.Location.ID, Instructions: []string{"Cook dinner."}}}}, nil
}

type campaignImageStub struct {
	calls int
	err   error
}

func (g *campaignImageStub) GenerateRecipeImage(context.Context, ai.Recipe) (*ai.GeneratedImage, error) {
	g.calls++
	if g.err != nil {
		return nil, g.err
	}
	return &ai.GeneratedImage{Body: strings.NewReader("campaign-image")}, nil
}

func testService() (*Service, *campaignGeneratorStub, *campaignImageStub) {
	c := cache.NewInMemoryCache()
	g, images := &campaignGeneratorStub{}, &campaignImageStub{}
	return &Service{
		locations: advertisedLocationStoreStub{}, generator: g, store: recipes.IO(c),
		statuses: status.NewStore(c), images: recipes.NewImageStore(c), imageGenerator: images, wait: func() {},
	}, g, images
}

func TestRunOnceGeneratesAndCachesAdvertisedRecipesAndImages(t *testing.T) {
	s, g, images := testService()
	waited := false
	s.wait = func() { waited = true }
	require.NoError(t, s.RunOnce(t.Context()))
	require.True(t, waited)
	require.Len(t, g.params, len(AdvertisedRecipeLocations()))
	require.Equal(t, len(g.params), images.calls)
	for i, p := range g.params {
		assert.Equal(t, "Hydrated "+p.Location.ID, p.Location.Name)
		session, ok := logsetup.SessionIDFromContext(g.contexts[i])
		require.True(t, ok)
		assert.Equal(t, "campaign_ads", session)
		user, ok := logsetup.UserIDFromContext(g.contexts[i])
		require.True(t, ok)
		assert.Equal(t, "campaign_ads", user)
		list, err := s.store.FromCache(t.Context(), p.Hash())
		require.NoError(t, err)
		require.Len(t, list.Recipes, 1)
		assert.Equal(t, []string{"Cook dinner."}, list.Recipes[0].Instructions)
		body, err := s.images.FromCache(t.Context(), list.Recipes[0].ComputeHash())
		require.NoError(t, err)
		data, err := io.ReadAll(body)
		require.NoError(t, err)
		require.NoError(t, body.Close())
		assert.Equal(t, "campaign-image", string(data))
	}
	require.NoError(t, s.RunOnce(t.Context()))
	assert.Len(t, g.params, len(AdvertisedRecipeLocations()))
	assert.Equal(t, len(g.params), images.calls)
}

func TestRunOnceReportsFailuresAndRetriesExistingParams(t *testing.T) {
	s, g, _ := testService()
	g.err = errors.New("flex unavailable")
	require.ErrorContains(t, s.RunOnce(t.Context()), "flex unavailable")
	require.Len(t, g.params, len(AdvertisedRecipeLocations()))
	for _, p := range g.params {
		state, err := s.statuses.Load(t.Context(), p.Hash())
		require.NoError(t, err)
		assert.Contains(t, state.Failed, "flex unavailable")
	}
	g.err = nil
	require.NoError(t, s.RunOnce(t.Context()))
	assert.Len(t, g.params, 2*len(AdvertisedRecipeLocations()))
}

func TestRunOnceRetriesMissingImagesWithoutRegeneratingRecipes(t *testing.T) {
	s, g, images := testService()
	images.err = errors.New("image unavailable")
	require.ErrorContains(t, s.RunOnce(t.Context()), "image unavailable")
	images.err = nil
	require.NoError(t, s.RunOnce(t.Context()))
	assert.Len(t, g.params, len(AdvertisedRecipeLocations()))
	assert.Equal(t, 2*len(g.params), images.calls)
	for _, p := range g.params {
		state, err := s.statuses.Load(t.Context(), p.Hash())
		require.NoError(t, err)
		assert.Empty(t, state.Failed)
	}
}

func TestGenerateDoesNotTreatCacheFailureAsMiss(t *testing.T) {
	s, g, _ := testService()
	s.store = failingCampaignStore{s.store}
	p := recipes.DefaultParams(&locations.Location{ID: "1"}, time.Now())
	require.ErrorContains(t, s.generate(t.Context(), p), "cache unavailable")
	assert.Empty(t, g.params)
}

type failingCampaignStore struct{ recipeStore }

func (f failingCampaignStore) FromCache(context.Context, string) (*ai.ShoppingList, error) {
	return nil, errors.New("cache unavailable")
}
