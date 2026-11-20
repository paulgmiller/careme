package campaigns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/locations"
	"careme/internal/logsetup"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/prompts"
	"careme/internal/recipes/status"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type advertisedLocationStore interface {
	GetLocationByID(context.Context, string) (*locations.Location, error)
}

type recipeGenerator interface {
	GenerateRecipes(context.Context, *recipes.GeneratorParams) (*ai.ShoppingList, error)
}

type recipeStore interface {
	FromCache(context.Context, string) (*ai.ShoppingList, error)
	SaveParams(context.Context, *recipes.GeneratorParams) error
	SaveShoppingList(context.Context, *ai.ShoppingList, string) error
}

type generationStatusStore interface {
	StartWithTimeout(context.Context, string, string, time.Duration) error
	Fail(context.Context, string, error) error
}

// service runs advertised recipe generation independently of the web server.
type service struct {
	locations advertisedLocationStore
	generator recipeGenerator
	store     recipeStore
	statuses  generationStatusStore
	wait      func()
}

type providerFactory interface {
	NewLocationBackends(locations.CentroidByZip) ([]locations.LocationBackend, error)
	NewStaplesBackends() ([]recipes.StaplesBackend, error)
}

func NewService(cfg *config.Config, providers providerFactory) (*service, error) {
	c, err := cache.MakeCache()
	if err != nil {
		return nil, fmt.Errorf("create campaign cache: %w", err)
	}
	imageCache, err := cache.EnsureCache(recipes.RecipeImagesContainer)
	if err != nil {
		return nil, fmt.Errorf("create campaign image cache: %w", err)
	}
	centroids := locations.LoadCentroids()
	locationBackends, err := providers.NewLocationBackends(centroids)
	if err != nil {
		return nil, fmt.Errorf("create campaign locations: %w", err)
	}
	locationStore, err := locations.New(c, centroids, locationBackends)
	if err != nil {
		return nil, fmt.Errorf("create campaign locations: %w", err)
	}
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	grader := ingredientgrading.NewManager(cfg, c, httpClient)
	backends, err := providers.NewStaplesBackends()
	if err != nil {
		return nil, fmt.Errorf("create campaign staples backends: %w", err)
	}
	staples := recipes.NewCachedStaplesService(backends, c, grader)
	aiConfig := cfg.AI
	aiConfig.ServiceTier = "flex"
	client := ai.NewClient(aiConfig, httpClient, prompts.NewCacheRecorder(c))
	critiquer := critique.NewManager(cfg, c, httpClient)
	statuses := status.NewStore(c)
	store := recipes.IO(c)
	imageJobs := recipes.NewRecipeImageGenerator(recipes.NewImageStore(imageCache), client)
	generator, err := recipes.NewGenerator(client, critiquer, staples, statuses, store, imageJobs)
	if err != nil {
		return nil, fmt.Errorf("create campaign generator: %w", err)
	}
	return &service{
		locations: locationStore, generator: generator, store: store,
		statuses: statuses, wait: critiquer.Wait,
	}, nil
}

// RunOnce waits for required recipes and images, returning failures to the cronjob.
func (s *service) RunOnce(ctx context.Context, now time.Time) error {
	defer s.wait()
	ctx = logsetup.WithSessionID(ctx, "campaign_ads")
	ctx = logsetup.WithUserID(ctx, "campaign_ads")
	var errs []error
	for _, c := range AdvertisedRecipeLocations() {
		if !c.Generate {
			continue
		}
		errs = append(errs, s.generateLocation(ctx, c, now))
	}
	return errors.Join(errs...)
}

func (s *service) generateLocation(ctx context.Context, c campaign, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, ai.FlexJobTimeout)
	defer cancel()
	locationID := c.Location.ID
	loc, err := s.locations.GetLocationByID(ctx, locationID)
	if err != nil {
		return fmt.Errorf("hydrate location %s: %w", locationID, err)
	}
	date, err := locations.StoreTime(ctx, now, loc)
	if err != nil {
		return fmt.Errorf("resolve store date for %s: %w", locationID, err)
	}
	if err := s.generate(ctx, recipes.DefaultParams(loc, date)); err != nil {
		return fmt.Errorf("generate campaign for %s: %w", locationID, err)
	}
	return nil
}

func (s *service) generate(ctx context.Context, p *recipes.GeneratorParams) error {
	hash := p.Hash()
	_, err := s.store.FromCache(ctx, hash)
	if err == nil {
		return nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return fmt.Errorf("read campaign shopping list: %w", err)
	}
	if err := s.statuses.StartWithTimeout(ctx, hash, status.InitialMessage, ai.FlexJobTimeout); err != nil {
		return fmt.Errorf("start campaign status: %w", err)
	}
	err = s.prepare(ctx, p)
	if err != nil {
		if statusErr := s.statuses.Fail(ctx, hash, err); statusErr != nil {
			return errors.Join(err, fmt.Errorf("record campaign failure: %w", statusErr))
		}
		return err
	}
	return nil
}

func (s *service) prepare(ctx context.Context, p *recipes.GeneratorParams) error {
	if err := s.store.SaveParams(ctx, p); err != nil && !errors.Is(err, recipes.ErrAlreadyExists) {
		return fmt.Errorf("save campaign params: %w", err)
	}
	list, err := s.generator.GenerateRecipes(ctx, p)
	if err != nil {
		return fmt.Errorf("generate campaign recipes: %w", err)
	}

	if len(list.Recipes) == 0 {
		return fmt.Errorf("campaign shopping list contains no recipes")
	}
	if err := s.store.SaveShoppingList(ctx, list, p.Hash()); err != nil {
		return fmt.Errorf("save campaign shopping list: %w", err)
	}

	return nil
}
