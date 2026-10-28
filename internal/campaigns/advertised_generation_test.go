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
	_ "careme/internal/providerregistry"
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
	if mnfoodclub.NewIdentityProvider().IsID(id) {
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

var generatedCampaignLocationIDs = []string{
	"wholefoods_10216", "70100023", "70100658",
	"mnfoodclub_delivery", "tcfarm_delivery", "nelsongrassfarm_delivery",
}

func TestRunOnceGeneratesAndCachesAdvertisedRecipesAndImages(t *testing.T) {
	s, g, images := testService()
	waited := false
	s.wait = func() { waited = true }
	require.NoError(t, s.RunOnce(t.Context(), campaignTestTime))
	require.True(t, waited)
	require.Len(t, g.params, len(generatedCampaignLocationIDs))
	require.Equal(t, len(g.params), images.calls)
	var generatedIDs []string
	for i, p := range g.params {
		generatedIDs = append(generatedIDs, p.Location.ID)
		loc, err := s.locations.GetLocationByID(t.Context(), p.Location.ID)
		require.NoError(t, err)
		assert.Equal(t, loc.Name, p.Location.Name)
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
	require.NoError(t, s.RunOnce(t.Context(), campaignTestTime))
	assert.Len(t, g.params, len(generatedCampaignLocationIDs))
	assert.Equal(t, len(g.params), images.calls)
}

func TestRunOnceReportsFailuresAndRetriesExistingParams(t *testing.T) {
	s, g, _ := testService()
	g.err = errors.New("flex unavailable")
	require.ErrorContains(t, s.RunOnce(t.Context(), campaignTestTime), "flex unavailable")
	require.Len(t, g.params, len(generatedCampaignLocationIDs))
	for _, p := range g.params {
		state, err := s.statuses.Load(t.Context(), p.Hash())
		require.NoError(t, err)
		assert.Contains(t, state.Failed, "flex unavailable")
	}
	g.err = nil
	require.NoError(t, s.RunOnce(t.Context(), campaignTestTime))
	assert.Len(t, g.params, 2*len(generatedCampaignLocationIDs))
}

func TestRunOnceRetriesMissingImagesWithoutRegeneratingRecipes(t *testing.T) {
	s, g, images := testService()
	images.err = errors.New("image unavailable")
	require.ErrorContains(t, s.RunOnce(t.Context(), campaignTestTime), "image unavailable")
	images.err = nil
	require.NoError(t, s.RunOnce(t.Context(), campaignTestTime))
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

func TestRunOnceGeneratesCampaignsAtAnyHourAndDay(t *testing.T) {
	for _, tt := range []struct {
		name    string
		now     string
		wantIDs []string
	}{
		{"winter Monday before Central 9 AM", "2026-01-05T14:59:00Z", generatedCampaignLocationIDs},
		{"winter Monday Central 9 AM", "2026-01-05T15:00:00Z", generatedCampaignLocationIDs},
		{"winter Monday Central 10 AM", "2026-01-05T16:00:00Z", generatedCampaignLocationIDs},
		{"winter Monday Pacific 10 AM", "2026-01-05T18:00:00Z", generatedCampaignLocationIDs},
		{"summer Monday before Central 9 AM", "2026-07-06T13:59:00Z", generatedCampaignLocationIDs},
		{"summer Monday Central 9 AM", "2026-07-06T14:00:00Z", generatedCampaignLocationIDs},
		{"summer Monday Pacific 10 AM", "2026-07-06T17:00:00Z", generatedCampaignLocationIDs},
		{"Tuesday", "2026-07-07T14:00:00Z", generatedCampaignLocationIDs},
		{"Wednesday", "2026-07-08T14:00:00Z", generatedCampaignLocationIDs},
		{"Thursday", "2026-07-09T14:00:00Z", generatedCampaignLocationIDs},
		{"Friday", "2026-07-10T14:00:00Z", generatedCampaignLocationIDs},
		{"Saturday", "2026-07-11T14:00:00Z", generatedCampaignLocationIDs},
		{"Sunday", "2026-07-12T14:00:00Z", generatedCampaignLocationIDs},
		{"Monday local midnight", "2026-07-06T05:00:00Z", generatedCampaignLocationIDs},
		{"UTC Monday local Sunday", "2026-07-06T04:59:00Z", generatedCampaignLocationIDs},
		{"UTC Tuesday local Monday night", "2026-07-07T04:59:00Z", generatedCampaignLocationIDs},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, g, images := testService()
			now, err := time.Parse(time.RFC3339, tt.now)
			require.NoError(t, err)
			require.NoError(t, s.RunOnce(t.Context(), now))
			var gotIDs []string
			for _, p := range g.params {
				gotIDs = append(gotIDs, p.Location.ID)
				assert.True(t, now.Equal(p.Date), "campaign params must preserve the generation instant")
				if mnfoodclub.NewIdentityProvider().IsID(p.Location.ID) {
					assert.Equal(t, "America/Chicago", p.Date.Location().String())
				}
			}
			assert.ElementsMatch(t, tt.wantIDs, gotIDs)
			assert.Equal(t, len(tt.wantIDs), images.calls)
			// Repeating the same instant reuses each store's current hash, even
			// when the initial run is immediately before a store-day boundary.
			require.NoError(t, s.RunOnce(t.Context(), now))
			assert.Len(t, g.params, len(tt.wantIDs))
			assert.Equal(t, len(tt.wantIDs), images.calls)
		})
	}
}

type campaignGeneratorFunc func(context.Context, *recipes.GeneratorParams) (*ai.ShoppingList, error)

func (f campaignGeneratorFunc) GenerateRecipes(ctx context.Context, p *recipes.GeneratorParams) (*ai.ShoppingList, error) {
	return f(ctx, p)
}

type blockingCampaignImages struct {
	recipes.ImageGen
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (g *blockingCampaignImages) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	g.once.Do(func() {
		close(g.started)
		<-g.release
	})
	return g.ImageGen.GenerateRecipeImage(ctx, recipe)
}

func TestRunOnceFinishesCampaignImagesBeforeStartingNextCampaign(t *testing.T) {
	s, generator, images := testService()
	started := make(chan string, len(generatedCampaignLocationIDs))
	s.generator = campaignGeneratorFunc(func(ctx context.Context, p *recipes.GeneratorParams) (*ai.ShoppingList, error) {
		started <- p.Location.ID
		return generator.GenerateRecipes(ctx, p)
	})
	blocked := &blockingCampaignImages{ImageGen: images, started: make(chan struct{}), release: make(chan struct{})}
	s.imageGenerator = blocked
	done := make(chan error, 1)
	go func() { done <- s.RunOnce(t.Context(), campaignTestTime) }()
	select {
	case <-blocked.started:
	case <-time.After(5 * time.Second):
		close(blocked.release)
		t.Fatal("first campaign did not reach image generation")
	}
	<-started
	select {
	case id := <-started:
		t.Errorf("campaign %s started before the previous campaign finished its images", id)
	case <-time.After(100 * time.Millisecond):
	}
	close(blocked.release)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("campaign generation did not finish")
	}
	assert.Len(t, generator.params, len(generatedCampaignLocationIDs))
	assert.Equal(t, len(generatedCampaignLocationIDs), images.calls)
}

func TestRunOnceRetriesMissingImagesAfter9AM(t *testing.T) {
	s, g, images := testService()
	images.err = errors.New("image unavailable")
	require.ErrorContains(t, s.RunOnce(t.Context(), campaignTestTime), "image unavailable")
	images.err = nil
	require.NoError(t, s.RunOnce(t.Context(), campaignTestTime.Add(2*time.Hour)))
	assert.Len(t, g.params, len(generatedCampaignLocationIDs))
	assert.Equal(t, 2*len(g.params), images.calls)
}

func TestRunOnceReusesWeeklyCampaignsOnFollowingDay(t *testing.T) {
	s, g, images := testService()
	require.NoError(t, s.RunOnce(t.Context(), campaignTestTime))
	require.NoError(t, s.RunOnce(t.Context(), campaignTestTime.AddDate(0, 0, 1)))
	counts := make(map[string]int)
	for _, p := range g.params {
		counts[p.Location.ID]++
	}
	for _, id := range generatedCampaignLocationIDs {
		want := 2
		if mnfoodclub.NewIdentityProvider().IsID(id) {
			want = 1
		}
		assert.Equal(t, want, counts[id], "generation count for %s", id)
	}
	// The stub returns the same recipes on both days, so images are reused too.
	assert.Equal(t, len(generatedCampaignLocationIDs), images.calls)
}
