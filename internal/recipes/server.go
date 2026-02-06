package recipes

import (
	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	"careme/internal/users"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/samber/lo"
)

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type generator interface {
	GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error)
	Ready(ctx context.Context) error
}

type server struct {
	recipeio
	cfg       *config.Config
	storage   *users.Storage
	cache     cache.Cache
	generator generator
	locServer locServer
	wg        sync.WaitGroup
	clerk     auth.AuthClient
}

// NewHandler returns an http.Handler serving the recipe endpoints under /recipes.
// cache must be connected to generator or this will not work. Should we enfroce that by getting cache from generator?
func NewHandler(cfg *config.Config, storage *users.Storage, generator generator, locServer locServer, c cache.Cache, clerkClient auth.AuthClient) *server {
	return &server{
		recipeio:  recipeio{Cache: c},
		cache:     c,
		cfg:       cfg,
		storage:   storage,
		generator: generator,
		locServer: locServer,
		clerk:     clerkClient,
	}
}

func (s *server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /recipes", s.handleRecipes)
	mux.HandleFunc("GET /recipe/{hash}", s.handleSingle)
	//maybe this should be under locations server?
	mux.HandleFunc("GET /ingredients/{location}", s.ingredients)

}

func (s *server) handleSingle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	recipe, err := s.SingleFromCache(ctx, hash)
	if err != nil {
		http.Error(w, "recipe not found", http.StatusNotFound)
		return
	}

	if recipe.OriginHash == "" {
		slog.WarnContext(ctx, "recipe missing origin hash Probably and old recipe", "hash", hash)
		p := DefaultParams(&locations.Location{
			ID:   "",
			Name: "Unknown Location",
		}, time.Now())
		FormatRecipeHTML(p, *recipe, w)
		return
	}

	p, err := loadParamsFromHash(ctx, recipe.OriginHash, s.cache)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load params for hash", "hash", recipe.OriginHash, "error", err)
		//http.Error(w, "recipe not found or expired", http.StatusNotFound)
		//return
		p = DefaultParams(&locations.Location{
			ID:   "",
			Name: "Unknown Location",
		}, time.Now())
	}

	// TODO this p is mising converastion id. See todo in generate recipes we can pregenerate it or update it after generation.

	// TODO: Add questions or regneration to signle recipes

	slog.InfoContext(ctx, "serving shared recipe by hash", "hash", hash)
	FormatRecipeHTML(p, *recipe, w)
}

const (
	queryArgHash  = "h"
	queryArgStart = "start"
)

func (s *server) notFound(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	startArg := r.URL.Query().Get(queryArgStart)
	hashParam := r.URL.Query().Get(queryArgHash)
	if startTime, err := time.Parse(time.RFC3339Nano, startArg); err == nil {
		if time.Since(startTime) > time.Minute*10 {
			p, err := loadParamsFromHash(ctx, hashParam, s.cache)
			if err != nil {
				slog.ErrorContext(ctx, "failed to load params for hash", "hash", hashParam, "error", err)
				http.Error(w, "recipe not found or expired", http.StatusNotFound)
				return
			}
			clerkUserID, err := s.clerk.GetUserIDFromRequest(r)
			if err != nil {
				if !errors.Is(err, auth.ErrNoSession) {
					slog.ErrorContext(ctx, "failed to get clerk user ID", "error", err)
					http.Error(w, "unable to load account", http.StatusInternalServerError)
					return
				}
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			currentUser, err := s.storage.FindOrCreateFromClerk(ctx, clerkUserID, s.clerk)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get user by clerk ID", "clerk_user_id", clerkUserID, "error", err)
				http.Error(w, "unable to load account", http.StatusInternalServerError)
				return
			}
			s.kickgeneration(ctx, p, currentUser)
			redirectToHash(w, r, p.Hash(), true /*useStart*/)
			return
		}
	}
	s.Spin(w, r)
}

func (s *server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if hashParam := r.URL.Query().Get(queryArgHash); hashParam != "" {
		slist, err := s.FromCache(ctx, hashParam) // ideally should memory cache this so lots of reloads don't constantly go out to azure
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				s.notFound(ctx, w, r)
				return
			}
			slog.ErrorContext(ctx, "failed to load recipe list for hash", "hash", hashParam, "error", err)
			http.Error(w, "recipe not found or expired", http.StatusNotFound)
			return
		}
		if r.URL.Query().Has(queryArgStart) {
			redirectToHash(w, r, hashParam, false /*useStart*/)
			return
		}

		p, err := loadParamsFromHash(ctx, hashParam, s.cache)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load params for hash", "hash", hashParam, "error", err)
			http.Error(w, "failed to load recipe parameters", http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("mail") == "true" {
			if err := FormatMail(p, *slist, w); err != nil {
				slog.ErrorContext(ctx, "failed to render mail template", "error", err)
				http.Error(w, "failed to render mail template", http.StatusInternalServerError)
			}
			return
		}
		FormatShoppingListHTML(p, *slist, w)
		return
	}

	p, err := s.ParseQueryArgs(ctx, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid query parameters: %v", err), http.StatusBadRequest)
		return
	}
	// what do we do with this?
	// p.UserID = currentUser.ID

	//if params are already saved redirect and assume someone kicks off genration

	if err := s.SaveParams(ctx, p); err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			slog.InfoContext(ctx, "params already existed redirecting", "hash", p.Hash())
			redirectToHash(w, r, p.Hash(), false /*useStart*/)
			return
		}
		slog.ErrorContext(ctx, "failed to save params", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	hash := p.Hash()

	clerkUserID, err := s.clerk.GetUserIDFromRequest(r)
	if err != nil {
		if !errors.Is(err, auth.ErrNoSession) {
			slog.ErrorContext(ctx, "failed to get clerk user ID", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		slog.InfoContext(ctx, "failed got no sesion from request", "error", err, "url", r.URL.String())
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	currentUser, err := s.storage.FindOrCreateFromClerk(ctx, clerkUserID, s.clerk)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get user by clerk ID", "clerk_user_id", clerkUserID, "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}

	// Handle finalize - save recipes to user profile and display filtered list
	if r.URL.Query().Get("finalize") == "true" {
		// Check if user is authenticated
		if currentUser.ID == "" {
			http.Error(w, "must be logged in to finalize recipes", http.StatusUnauthorized)
			return
		}

		// If no recipes are saved, just return to home
		if len(p.Saved) == 0 {
			http.Error(w, "no recipes selected to save", http.StatusBadRequest)
			return
		}

		// Save recipes to user profile
		if err := s.saveRecipesToUserProfile(ctx, currentUser.ID, p.Saved); err != nil {
			slog.ErrorContext(ctx, "failed to save recipes to user profile", "user_id", currentUser.ID, "error", err)
			http.Error(w, "failed to save recipes", http.StatusInternalServerError)
			return
		}
		slog.InfoContext(ctx, "finalized recipes", "user_id", currentUser.ID, "count", len(p.Saved))

		// Display the saved recipes
		shoppingList := &ai.ShoppingList{
			Recipes:        p.Saved,
			ConversationID: p.ConversationID,
		}

		// should finalize go into params to get a different hash that previous one with unsaved?
		// or should we shove a guid or iteration in params along with conversation id. Response id?
		if err := s.SaveShoppingList(ctx, shoppingList, hash); err != nil {
			slog.ErrorContext(ctx, "save error", "error", err)
			http.Error(w, "failed to save finalized recipes", http.StatusInternalServerError)
			return
		}
		redirectToHash(w, r, hash, false /*useStart*/)
		return
	}

	s.kickgeneration(ctx, p, currentUser)

	redirectToHash(w, r, hash, true /*useStart*/)
}

func (s *server) kickgeneration(ctx context.Context, p *generatorParams, currentUser *users.User) {
	for _, last := range currentUser.LastRecipes {
		if last.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
			break
		}
		p.LastRecipes = append(p.LastRecipes, last.Title)
	}

	hash := p.Hash()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// copy over request id to new context? can't be same context because end of http request will cancel it.
		ctx := context.Background()
		slog.InfoContext(ctx, "generating cached recipes", "params", p.String(), "hash", hash)
		shoppingList, err := s.generator.GenerateRecipes(ctx, p)
		if err != nil {
			slog.ErrorContext(ctx, "generate error", "error", err)
			return
		}

		// add saved recipes here rather than each

		if err := s.SaveShoppingList(ctx, shoppingList, hash); err != nil {
			slog.ErrorContext(ctx, "save error", "error", err)
		}
		// saveRecipesToUserProfile saves recipes to the user profile if they were marked as saved.

		// Use the current user ID when saving recipes to the user profile
		// needs user to be logged in. Only do on finalize?
		if currentUser.ID != "" {
			if err := s.saveRecipesToUserProfile(ctx, currentUser.ID, p.Saved); err != nil {
				slog.ErrorContext(ctx, "failed to save recipes to user profile", "user_id", currentUser.ID, "error", err)
			}
		}
	}()
}

func (s *server) Spin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	ctx := r.Context()
	spinnerData := struct {
		ClarityScript   template.HTML
		Style           seasons.Style
		RefreshInterval string // seconds
	}{
		ClarityScript:   templates.ClarityScript(),
		Style:           seasons.GetCurrentStyle(),
		RefreshInterval: "10", // seconds
	}

	if err := templates.Spin.Execute(w, spinnerData); err != nil {
		slog.ErrorContext(ctx, "home template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func redirectToHash(w http.ResponseWriter, r *http.Request, hash string, useStart bool) {
	u := url.URL{Path: "/recipes"}
	args := url.Values{}
	args.Set(queryArgHash, hash)
	if useStart {
		args.Set(queryArgStart, time.Now().Format(time.RFC3339Nano))
	}
	u.RawQuery = args.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func (s *server) Wait() {
	s.wg.Wait()
}

// saveRecipesToUserProfile adds saved recipes to the user's profile
func (s *server) saveRecipesToUserProfile(ctx context.Context, userID string, savedRecipes []ai.Recipe) error {
	if userID == "" {
		return fmt.Errorf("invalid user")
	}

	if len(savedRecipes) == 0 {
		return nil
	}

	// Reload the user to get the latest state
	currentUser, err := s.storage.GetByID(userID)
	if err != nil {
		return fmt.Errorf("failed to reload user: %w", err)
	}

	// Track if any new recipes were added
	added := 0
	addTime := time.Now()
	for _, recipe := range savedRecipes {
		// Check if recipe already exists in user's last recipes
		hash := recipe.ComputeHash()

		_, exists := lo.Find(currentUser.LastRecipes, func(r users.Recipe) bool {
			return r.Hash == hash
		})
		if exists {
			continue
		}
		newRecipe := users.Recipe{
			Title:     recipe.Title,
			Hash:      hash,
			CreatedAt: addTime,
		}
		currentUser.LastRecipes = append(currentUser.LastRecipes, newRecipe)
		added++
		slog.InfoContext(ctx, "added saved recipe to user profile", "user_id", userID, "title", recipe.Title)
	}

	if added > 0 {
		// etag mismatch fun!
		if err := s.storage.Update(currentUser); err != nil {
			return fmt.Errorf("failed to update user with saved recipes: %w", err)
		}
		slog.InfoContext(ctx, "saved recipes to user profile", "user_id", userID, "count", added)
	}

	return nil
}

// move to admin? Nah let the people see
func (s *server) ingredients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc := r.PathValue("location")
	l, err := s.locServer.GetLocationByID(ctx, loc)
	if err != nil {
		http.Error(w, "invalid location id", http.StatusBadRequest)
		return
	}
	// later use saved items
	p := DefaultParams(l, time.Now())

	lochash := p.LocationHash()
	ingredientblob, err := s.cache.Get(ctx, lochash)
	if err != nil {
		http.Error(w, "ingredients not found in cache", http.StatusNotFound)
		return
	}
	slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash)
	defer func() {
		if err := ingredientblob.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached ingredients", "location", p.String(), "error", err)
		}
	}()
	dec := json.NewDecoder(ingredientblob)
	var ingredients []kroger.Ingredient
	err = dec.Decode(&ingredients)
	if err != nil {
		http.Error(w, "failed to decode ingredients", http.StatusInternalServerError)
		return
	}
	// make this a html thats readable.
	w.Header().Add("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ingredients); err != nil {
		http.Error(w, "failed to encode ingredients", http.StatusInternalServerError)
		return
	}
}
