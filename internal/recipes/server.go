package recipes

import (
	"careme/internal/ai"
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
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
)

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type generator interface {
	GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error)
}

type server struct {
	recipeio
	cfg       *config.Config
	storage   *users.Storage
	cache     cache.Cache
	generator generator
	locServer locServer
	wg        sync.WaitGroup
}

// NewHandler returns an http.Handler serving the recipe endpoints under /recipes.
// cache must be connected to generator or this will not work. Should we enfroce that by getting cache from generator?
func NewHandler(cfg *config.Config, storage *users.Storage, generator generator, locServer locServer, c cache.Cache) *server {
	return &server{
		recipeio:  recipeio{Cache: c},
		cache:     c,
		cfg:       cfg,
		storage:   storage,
		generator: generator,
		locServer: locServer,
	}
}

func (s *server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /recipes", s.handleRecipes)
	mux.HandleFunc("GET /recipe/{hash}", s.handleSingle)
	mux.HandleFunc("POST /recipes/finalize", s.handleFinalize)
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

	p := DefaultParams(&locations.Location{
		ID:   "",
		Name: "Unknown Location",
	}, time.Now())
	if recipe.OriginHash != "" {
		loadedp, err := loadParamsFromHash(ctx, recipe.OriginHash, s.cache)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load params for hash", "hash", recipe.OriginHash, "error", err)
			// http.Error(w, "recipe not found or expired", http.StatusNotFound)
			// return
		} else {
			p = loadedp
		}
	}

	list := ai.ShoppingList{
		Recipes: []ai.Recipe{*recipe},
	}

	slog.InfoContext(ctx, "serving shared recipe by hash", "hash", hash)
	FormatChatHTML(p, list, w)
}

func (s *server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser, err := users.FromRequest(r, s.storage)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			users.ClearCookie(w)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for recipes", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	if currentUser == nil {
		currentUser = &users.User{LastRecipes: []users.Recipe{}}
	}

	if hashParam := r.URL.Query().Get("h"); hashParam != "" {
		slist, err := s.FromCache(ctx, hashParam)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load recipe list for hash", "hash", hashParam, "error", err)
			http.Error(w, "recipe not found or expired", http.StatusNotFound)
			return
		}
		p, err := loadParamsFromHash(ctx, hashParam, s.cache)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load params for hash", "hash", hashParam, "error", err)
			p = DefaultParams(&locations.Location{
				ID:   "",
				Name: "Unknown Location",
			}, time.Now())
		}
		FormatChatHTML(p, *slist, w)
		// backfill
		go func() {
			cutoff := lo.Must(time.Parse(time.DateOnly, "2025-12-22"))
			if p.Date.After(cutoff) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			// nothing we can do on failure anyways. Aleaady logged
			_ = s.SaveRecipes(ctx, slist.Recipes, p.Hash())
		}()
		return
	}

	p, err := s.ParseQueryArgs(ctx, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid query parameters: %v", err), http.StatusBadRequest)
		return
	}
	// what do we do with this?
	// p.UserID = currentUser.ID

	if r.URL.Query().Get("ingredients") == "true" {
		s.ingredients(ctx, w, p)
		return
	}

	for _, last := range currentUser.LastRecipes {
		if last.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
			break
		}
		p.LastRecipes = append(p.LastRecipes, last.Title)
	}

	hash := p.Hash()
	if list, err := s.FromCache(ctx, hash); err == nil {
		// TODO check not found error explicitly
		if r.URL.Query().Get("mail") == "true" {
			FormatMail(p, *list, w)
			return
		}
		FormatChatHTML(p, *list, w)
		// backfill
		go func() {
			cutoff := lo.Must(time.Parse(time.DateOnly, "2025-12-22"))
			if p.Date.After(cutoff) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			// nothing we can do on failure anyways. Aleaady logged
			_ = s.SaveRecipes(ctx, list.Recipes, p.Hash())
		}()
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// copy over request id to new context? can't be same context because end of http request will cancel it.
		ctx := context.Background()
		slog.InfoContext(ctx, "generating cached recipes", "params", p.String(), "hash", hash)
		shoppingList, err := s.generator.GenerateRecipes(ctx, p)
		if err != nil {
			if errors.Is(err, InProgress) {
				slog.InfoContext(ctx, "generation already in progress, skipping save", "hash", hash)
				return
			}
			slog.ErrorContext(ctx, "generate error", "error", err)
			return
		}

		// add saved recipes here rather than each

		if err := s.SaveShoppingList(ctx, shoppingList, p); err != nil {
			slog.ErrorContext(ctx, "save error", "error", err)
		}
		// saveRecipesToUserProfile saves recipes to the user profile if they were marked as saved.

		// Use the current user ID when saving recipes to the user profile
		if err := s.saveRecipesToUserProfile(ctx, currentUser.ID, p.Saved); err != nil {
			slog.ErrorContext(ctx, "failed to save recipes to user profile", "user_id", currentUser.ID, "error", err)
		}
	}()
	// TODO should we just redirect to cache page here?
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
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

// handleFinalize saves the selected recipes to the user profile without regenerating
func (s *server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser, err := users.FromRequest(r, s.storage)
	if err != nil {
		if errors.Is(err, users.ErrNotFound) {
			users.ClearCookie(w)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for finalize", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	if currentUser == nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	// Parse form data to get saved recipe hashes
	if err := r.ParseForm(); err != nil {
		slog.ErrorContext(ctx, "failed to parse form", "error", err)
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	// Get saved recipe hashes from form
	clean := func(s string, _ int) (string, bool) {
		ts := strings.TrimSpace(s)
		return ts, ts != ""
	}
	savedHashes := lo.FilterMap(r.Form["saved"], clean)

	if len(savedHashes) == 0 {
		slog.InfoContext(ctx, "no recipes to finalize", "user_id", currentUser.ID)
		// Redirect back to home or recipes page
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Load saved recipes from cache
	var savedRecipes []ai.Recipe
	for _, hash := range savedHashes {
		recipe, err := s.SingleFromCache(ctx, hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load recipe for finalize", "hash", hash, "error", err)
			continue
		}
		savedRecipes = append(savedRecipes, *recipe)
	}

	// Save recipes to user profile
	if err := s.saveRecipesToUserProfile(ctx, currentUser.ID, savedRecipes); err != nil {
		slog.ErrorContext(ctx, "failed to save recipes to user profile", "user_id", currentUser.ID, "error", err)
		http.Error(w, "failed to save recipes", http.StatusInternalServerError)
		return
	}

	slog.InfoContext(ctx, "finalized recipes", "user_id", currentUser.ID, "count", len(savedRecipes))

	// Redirect back to home
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// move to admin?
func (s *server) ingredients(ctx context.Context, w http.ResponseWriter, p *generatorParams) {
	lochash := p.LocationHash()
	ingredientblob, err := s.cache.Get(ctx, lochash)
	if err != nil {
		http.Error(w, "ingredients not found in cache", http.StatusNotFound)
		return
	}
	slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash)
	defer ingredientblob.Close()
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
