package campaigns

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/providers/mnfoodclub"
	"careme/internal/recipes"
	"careme/internal/recipes/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type advertisedLocationStoreStub struct{}

func (advertisedLocationStoreStub) GetLocationByID(ctx context.Context, id string) (*locations.Location, error) {
	if id == "smithbrothersfarms_delivery" {
		return nil, errors.New("disabled campaign should not hydrate its location")
	}
	if strings.HasPrefix(id, mnfoodclub.LocationIDPrefix) {
		return mnfoodclub.NewLocationBackend().GetLocationByID(ctx, id)
	}
	lat, lon := 47.61, -122.33
	return &locations.Location{ID: id, Name: "Hydrated " + id, Address: id + " Market St", ZipCode: "98101", Lat: &lat, Lon: &lon}, nil
}

type campaignGeneratorStub struct {
	mu       sync.Mutex
	params   []*recipes.GeneratorParams
	contexts []context.Context
	err      error
}

func (g *campaignGeneratorStub) GenerateRecipes(ctx context.Context, p *recipes.GeneratorParams) (*ai.ShoppingList, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.params = append(g.params, p)
	g.contexts = append(g.contexts, ctx)
	if g.err != nil {
		return nil, g.err
	}
	return &ai.ShoppingList{Recipes: []ai.Recipe{{Title: "Dinner at " + p.Location.ID, Instructions: []string{"Cook dinner."}}}}, nil
}

type campaignImageStub struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (g *campaignImageStub) GenerateRecipeImage(context.Context, ai.Recipe) (*ai.GeneratedImage, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.err != nil {
		return nil, g.err
	}
	return &ai.GeneratedImage{Body: strings.NewReader("campaign-image")}, nil
}

func (g *campaignImageStub) GenerateStepImage(context.Context, ai.Recipe, int) (*ai.GeneratedImage, error) {
	panic("unexpected step image request")
}

func testService() (*Service, *campaignGeneratorStub, *campaignImageStub) {
	c := cache.NewInMemoryCache()
	g, images := &campaignGeneratorStub{}, &campaignImageStub{}
	return &Service{
		locations: advertisedLocationStoreStub{}, generator: g, store: recipes.IO(c),
		statuses: status.NewStore(c), images: recipes.NewImageStore(c), imageGenerator: images, wait: func() {},
	}, g, images
}

// Wednesday at the production job hour (9 AM Pacific).
var campaignTestTime = time.Date(2026, 10, 21, 16, 0, 0, 0, time.UTC)

var generatedCampaignLocationIDs = []string{"wholefoods_10216", "70100023", "70100658"}

func TestRunOnceGeneratesAndCachesAdvertisedRecipesAndImages(t *testing.T) {
	s, g, images := testService()
	waited := false
	s.wait = func() { waited = true }
	require.NoError(t, s.runOnce(t.Context(), campaignTestTime))
	require.True(t, waited)
	require.Len(t, g.params, len(generatedCampaignLocationIDs))
	require.Equal(t, len(g.params), images.calls)
	var generatedIDs []string
	for i, p := range g.params {
		generatedIDs = append(generatedIDs, p.Location.ID)
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
	assert.ElementsMatch(t, generatedCampaignLocationIDs, generatedIDs)
	require.NoError(t, s.runOnce(t.Context(), campaignTestTime))
	assert.Len(t, g.params, len(generatedCampaignLocationIDs))
	assert.Equal(t, len(g.params), images.calls)
}

func TestRunOnceReportsFailuresAndRetriesExistingParams(t *testing.T) {
	campaignTestTime := time.Date(2026, 7, 6, 14, 0, 0, 0, time.UTC) // Monday, 9 AM Central.
	s, g, _ := testService()
	g.err = errors.New("flex unavailable")
	require.ErrorContains(t, s.runOnce(t.Context(), campaignTestTime), "flex unavailable")
	require.Len(t, g.params, len(generatedCampaignLocationIDs))
	for _, p := range g.params {
		state, err := s.statuses.Load(t.Context(), p.Hash())
		require.NoError(t, err)
		assert.Contains(t, state.Failed, "flex unavailable")
	}
	g.err = nil
	require.NoError(t, s.runOnce(t.Context(), campaignTestTime))
	assert.Len(t, g.params, 2*len(generatedCampaignLocationIDs))
}

func TestRunOnceRetriesMissingImagesWithoutRegeneratingRecipes(t *testing.T) {
	campaignTestTime := time.Date(2026, 7, 6, 14, 0, 0, 0, time.UTC) // Monday, 9 AM Central.
	s, g, images := testService()
	images.err = errors.New("image unavailable")
	require.ErrorContains(t, s.runOnce(t.Context(), campaignTestTime), "image unavailable")
	images.err = nil
	require.NoError(t, s.runOnce(t.Context(), campaignTestTime))
	assert.Len(t, g.params, len(generatedCampaignLocationIDs))
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

func TestRunOnceGeneratesOnlyCampaignsDueAtLocal9AM(t *testing.T) {
	mnfoodclubIDs := []string{"mnfoodclub_delivery", "mnfoodclub_tcfarm_delivery", "mnfoodclub_nelsongrassfarm_delivery"}
	for _, tt := range []struct {
		name    string
		now     string
		wantIDs []string
	}{
		{"winter Monday before Central 9 AM", "2026-01-05T14:59:00Z", nil},
		{"winter Monday Central 9 AM", "2026-01-05T15:00:00Z", mnfoodclubIDs},
		{"winter Monday Central delayed start", "2026-01-05T15:49:00Z", mnfoodclubIDs},
		{"winter Monday Central 10 AM", "2026-01-05T16:00:00Z", nil},
		{"winter Monday Pacific 9 AM", "2026-01-05T17:00:00Z", generatedCampaignLocationIDs},
		{"winter Monday Pacific delayed start", "2026-01-05T17:49:00Z", generatedCampaignLocationIDs},
		{"winter Monday Pacific 10 AM", "2026-01-05T18:00:00Z", nil},
		{"summer Monday before Central 9 AM", "2026-07-06T13:59:00Z", nil},
		{"summer Monday Central 9 AM", "2026-07-06T14:00:00Z", mnfoodclubIDs},
		{"summer Monday Central 10 AM", "2026-07-06T15:00:00Z", nil},
		{"summer Monday Pacific 9 AM", "2026-07-06T16:00:00Z", generatedCampaignLocationIDs},
		{"summer Monday Pacific 10 AM", "2026-07-06T17:00:00Z", nil},
		{"Tuesday Central 9 AM", "2026-07-07T14:00:00Z", nil},
		{"Wednesday Central 9 AM", "2026-07-08T14:00:00Z", nil},
		{"Thursday Central 9 AM", "2026-07-09T14:00:00Z", nil},
		{"Friday Central 9 AM", "2026-07-10T14:00:00Z", nil},
		{"Saturday Central 9 AM", "2026-07-11T14:00:00Z", nil},
		{"Sunday Central 9 AM", "2026-07-12T14:00:00Z", nil},
		{"Sunday Pacific 9 AM", "2026-07-12T16:00:00Z", generatedCampaignLocationIDs},
		{"Monday local midnight", "2026-07-06T05:00:00Z", nil},
		{"UTC Tuesday local Monday night", "2026-07-07T04:59:00Z", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, g, images := testService()
			now, err := time.Parse(time.RFC3339, tt.now)
			require.NoError(t, err)
			require.NoError(t, s.runOnce(t.Context(), now))
			var gotIDs []string
			for _, p := range g.params {
				gotIDs = append(gotIDs, p.Location.ID)
				assert.Equal(t, now.In(p.Date.Location()).Format(time.DateOnly), p.Date.Format(time.DateOnly))
				if strings.HasPrefix(p.Location.ID, mnfoodclub.LocationIDPrefix) {
					assert.Equal(t, "America/Chicago", p.Date.Location().String())
				}
			}
			assert.ElementsMatch(t, tt.wantIDs, gotIDs)
			assert.Equal(t, len(tt.wantIDs), images.calls)
			// Repeated starts in the same local 9 AM hour reuse recipes and images.
			retryAt := now
			if len(tt.wantIDs) > 0 {
				retryAt = now.Add(10 * time.Minute)
			}
			require.NoError(t, s.runOnce(t.Context(), retryAt))
			assert.Len(t, g.params, len(tt.wantIDs))
			assert.Equal(t, len(tt.wantIDs), images.calls)
		})
	}
}
