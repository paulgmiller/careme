package recipes

import (
	"context"
	"io"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/status"
	"careme/internal/routing"
	"careme/internal/users"
)

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type generator interface {
	GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error)
	RegenerateRecipe(ctx context.Context, instructions []string, previous ai.ResponseRef) (*ai.Recipe, error)
	AskQuestion(ctx context.Context, question string, previous ai.ResponseRef) (*ai.QuestionResponse, error)
	PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error)
}

type ExtGenerator = generator

// should probably be in ai package?
type ImageGen interface {
	GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error)
}

type ImageStore interface {
	Exists(ctx context.Context, hash string) (bool, error)
	FromCache(ctx context.Context, hash string) (io.ReadCloser, error)
	Save(ctx context.Context, hash string, image *ai.GeneratedImage) error
}

type statusStore interface {
	Start(ctx context.Context, hash, message string) error
	Update(ctx context.Context, hash, message string) error
	Fail(ctx context.Context, hash string, err error) error
	Load(ctx context.Context, hash string) (status.Status, error)
	Complete(ctx context.Context, id, newHash string) error
}

type server struct {
	recipeio
	images             ImageStore
	imagegen           ImageGen
	generationStatuses statusStore
	cfg                *config.Config
	storage            *users.Storage
	generator          generator
	locServer          locServer
	wg                 sync.WaitGroup
	clerk              auth.AuthClient
	critiques          critiqueStore
}

type critiqueStore interface {
	Load(ctx context.Context, hash string) (*ai.RecipeCritique, error)
}

// NewHandler returns an http.Handler serving the recipe endpoints under /recipes.
// cache must be connected to generator or this will not work. Should we enfroce that by getting cache from generator?
func NewHandler(cfg *config.Config, storage *users.Storage, generator generator, locServer locServer, c cache.ListCache, imageCache cache.Cache, clerkClient auth.AuthClient, imagegen ImageGen) *server {
	return &server{
		recipeio:           IO(c),
		images:             NewImageStore(imageCache),
		imagegen:           imagegen,
		generationStatuses: status.NewStore(c),
		cfg:                cfg,
		storage:            storage,
		generator:          generator,
		locServer:          locServer,
		clerk:              clerkClient,
		critiques:          critique.NewStore(c),
	}
}

func (s *server) Register(mux routing.Registrar) {
	s.registerRecipeRoutes(mux)
	s.registerShoppingListRoutes(mux)
	// save/dimsiss
	s.registerSelectionRoutes(mux)
}

func (s *server) Wait() {
	s.wg.Wait()
}
