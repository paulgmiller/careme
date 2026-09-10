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

	"github.com/samber/lo"
	lop "github.com/samber/lo/parallel"

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

// Service runs advertised recipe generation independently of the web server.
type Service struct {
	locations      advertisedLocationStore
	generator      recipeGenerator
	store          recipeStore
	statuses       *status.Store
	images         recipes.ImageStore
	imageGenerator recipes.ImageGen
	wait           func()
}

func NewService(cfg *config.Config) (*Service, error) {
	c, err := cache.MakeCache()
	if err != nil {
		return nil, fmt.Errorf("create campaign cache: %w", err)
	}
	imageCache, err := cache.EnsureCache(recipes.RecipeImagesContainer)
	if err != nil {
		return nil, fmt.Errorf("create campaign image cache: %w", err)
	}
	locationStore, err := locations.New(cfg, c, locations.LoadCentroids())
	if err != nil {
		return nil, fmt.Errorf("create campaign locations: %w", err)
	}
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	grader := ingredientgrading.NewManager(cfg, c, httpClient)
	staples, err := recipes.NewCachedStaplesService(cfg, c, grader)
	if err != nil {
		return nil, fmt.Errorf("create campaign staples: %w", err)
	}
	aiConfig := cfg.AI
	aiConfig.ServiceTier = "flex"
	client := ai.NewClient(aiConfig, httpClient, prompts.NewCacheRecorder(c))
	critiquer := critique.NewManager(cfg, c, httpClient)
	statuses := status.NewStore(c)
	store := recipes.IO(c)
	generator, err := recipes.NewGenerator(client, critiquer, staples, statuses, store)
	if err != nil {
		return nil, fmt.Errorf("create campaign generator: %w", err)
	}
	return &Service{
		locations: locationStore, generator: generator, store: store,
		statuses: statuses, images: recipes.NewImageStore(imageCache), imageGenerator: client, wait: critiquer.Wait,
	}, nil
}

// RunOnce waits for required recipes and images, returning failures to the cronjob.
func (s *Service) RunOnce(ctx context.Context) error {
	defer s.wait()
	ctx = logsetup.WithSessionID(ctx, "campaign_ads")
	ctx = logsetup.WithUserID(ctx, "campaign_ads")
	errs := lop.Map(lo.Values(AdvertisedRecipeLocations()), func(c campaign, _ int) error {
		return s.generateLocation(ctx, c.Location.ID)
	})
	return errors.Join(errs...)
}

func (s *Service) generateLocation(ctx context.Context, locationID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	loc, err := s.locations.GetLocationByID(ctx, locationID)
	if err != nil {
		return fmt.Errorf("hydrate location %s: %w", locationID, err)
	}
	date, err := recipes.StoreToDate(ctx, time.Now(), loc)
	if err != nil {
		return fmt.Errorf("resolve store date for %s: %w", locationID, err)
	}
	if err := s.generate(ctx, recipes.DefaultParams(loc, date)); err != nil {
		return fmt.Errorf("generate campaign for %s: %w", locationID, err)
	}
	return nil
}

func (s *Service) generate(ctx context.Context, p *recipes.GeneratorParams) error {
	hash := p.Hash()
	list, err := s.store.FromCache(ctx, hash)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return fmt.Errorf("read campaign shopping list: %w", err)
	}
	missing := errors.Is(err, cache.ErrNotFound)
	if err := s.statuses.Start(ctx, hash); err != nil {
		return fmt.Errorf("start campaign status: %w", err)
	}

	if !missing { // small chance someone got to this locationb before us?
		if err := s.prepareImage(ctx, list); err != nil {
			return err
		}
		return nil
	}

	if err := s.prepare(ctx, p); err != nil {
		if statusErr := s.statuses.Fail(ctx, hash, err); statusErr != nil {
			return errors.Join(err, fmt.Errorf("record campaign failure: %w", statusErr))
		}
		return err
	}

	return nil
}

func (s *Service) prepare(ctx context.Context, p *recipes.GeneratorParams) error {
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

	if err := s.prepareImage(ctx, list); err != nil {
		return err
	}

	return nil
}

func (s *Service) prepareImage(ctx context.Context, list *ai.ShoppingList) error {
	errs := lop.Map(list.Recipes, func(recipe ai.Recipe, _ int) error {
		// magic number for timeout
		ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		hash := recipe.ComputeHash()
		exists, err := s.images.Exists(ctx, hash)
		if err != nil {
			return fmt.Errorf("check image cache: %w", err)
		}
		if exists {
			return nil
		}
		image, err := s.imageGenerator.GenerateRecipeImage(ctx, recipe)
		if err != nil {
			return fmt.Errorf("generate image: %w", err)
		}
		if err := s.images.Save(ctx, hash, image); err != nil {
			return fmt.Errorf("save image: %w", err)
		}
		return nil
	})
	return errors.Join(errs...)
}
