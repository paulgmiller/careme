package recipes

import (
	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes/generation"
	"careme/internal/recipes/selectionstate"
	"careme/internal/users"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func setTextContent(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type generator interface {
	GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error)
	AskQuestion(ctx context.Context, question string, conversationID string) (string, error)
	Ready(ctx context.Context) error
}

type server struct {
	recipeio
	cfg            *config.Config
	storage        *users.Storage
	cache          cache.Cache
	generator      generator
	locServer      locServer
	selectionStore *selectionstate.Store
	genRunner      *generation.Runner
	clerk          auth.AuthClient
}

// NewHandler returns an http.Handler serving the recipe endpoints under /recipes.
// cache must be connected to generator or this will not work. Should we enfroce that by getting cache from generator?
func NewHandler(cfg *config.Config, storage *users.Storage, generator generator, locServer locServer, c cache.Cache, clerkClient auth.AuthClient) *server {
	return &server{
		recipeio:       recipeio{Cache: c},
		cache:          c,
		cfg:            cfg,
		storage:        storage,
		generator:      generator,
		locServer:      locServer,
		selectionStore: selectionstate.NewStore(c),
		genRunner:      generation.NewRunner(),
		clerk:          clerkClient,
	}
}

func (s *server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /recipes", s.handleRecipes)
	mux.HandleFunc("POST /recipes/{hash}/regenerate", s.handleRegenerate)
	mux.HandleFunc("POST /recipes/{hash}/finalize", s.handleFinalize)
	mux.HandleFunc("GET /recipe/{hash}", s.handleSingle)
	mux.HandleFunc("POST /recipe/{hash}/question", s.handleQuestion)
	mux.HandleFunc("POST /recipe/{hash}/feedback", s.handleFeedback)
	mux.HandleFunc("POST /recipe/{hash}/save", s.handleSaveRecipe)
	mux.HandleFunc("POST /recipe/{hash}/dismiss", s.handleDismissRecipe)
	//maybe this should be under locations server?
	mux.HandleFunc("GET /ingredients/{location}", s.ingredients)
}

const (
	queryArgHash  = "h"
	queryArgStart = "start"
)

func redirectToHash(w http.ResponseWriter, r *http.Request, hash string, useStart bool) {
	u := url.URL{Path: "/recipes"}
	args := url.Values{} // intentioanlly clear other args
	args.Set(queryArgHash, hash)
	if useStart {
		args.Set(queryArgStart, time.Now().Format(time.RFC3339Nano))
	}
	u.RawQuery = args.Encode()
	if isHTMXRequest(r) {
		w.Header().Set("HX-Redirect", u.String())
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func isHTMXRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

func parseFeedbackBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true, nil
	case "", "0", "false", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean: %q", value)
	}
}
