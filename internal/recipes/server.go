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
		loadedp, err := s.loadParamsFromHash(ctx, recipe.OriginHash)
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
		p, err := s.loadParamsFromHash(ctx, hashParam)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load params for hash", "hash", hashParam, "error", err)
			p = DefaultParams(&locations.Location{
				ID:   "",
				Name: "Unknown Location",
			}, time.Now())
		}
		FormatChatHTML(p, *slist, w)
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

	loc := r.URL.Query().Get("location")
	if loc == "" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("specify a location id to generate recipes"))
		return
	}

	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		http.Redirect(w, r, "/recipes?location="+loc+"&date="+time.Now().Format("2006-01-02"), http.StatusSeeOther)
		return
	}

	date, err := time.ParseInLocation("2006-01-02", dateStr, time.UTC)
	if err != nil {
		http.Error(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
		return
	}

	l, err := s.locServer.GetLocationByID(ctx, loc)
	if err != nil {
		http.Error(w, "could not get location details", http.StatusBadRequest)
		return
	}

	p := DefaultParams(l, date)

	p.UserID = currentUser.ID

	if r.URL.Query().Get("ingredients") == "true" {
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
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(ingredients); err != nil {
			http.Error(w, "failed to encode ingredients", http.StatusInternalServerError)
			return
		}
		// make this a html thats readable.
		w.Header().Add("Content-Type", "application/json")
		return
	}

	for _, last := range currentUser.LastRecipes {
		if last.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
			break
		}
		p.LastRecipes = append(p.LastRecipes, last.Title)
	}

	if instructions := r.URL.Query().Get("instructions"); instructions != "" {
		p.Instructions = instructions
	}

	// Handle saved and dismissed recipe hashes from checkboxes
	// Query().Get returns first value, Query() returns all values
	// will be empty values for every recipe and two for ones with no action
	// TODO look at way not to duplicate so many query arguments and pass down just a saved list or a query arg for each saved item.
	clean := func(s string, _ int) (string, bool) {
		ts := strings.TrimSpace(s)
		return ts, ts != ""
	}
	savedHashes := lo.FilterMap(r.URL.Query()["saved"], clean)
	dismissedHashes := lo.FilterMap(r.URL.Query()["dismissed"], clean)
	// Load saved recipes from cache by their hashes
	for _, hash := range savedHashes {
		recipe, err := s.SingleFromCache(ctx, hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load saved recipe by hash", "hash", hash, "error", err)
			continue
		}
		slog.InfoContext(ctx, "adding saved recipe to params", "title", recipe.Title, "hash", hash)
		p.Saved = append(p.Saved, *recipe)
	}

	// Add dismissed recipe titles to instructions so AI knows what to avoid
	for _, hash := range dismissedHashes {
		recipe, err := s.SingleFromCache(ctx, hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load dismissed recipe by hash", "hash", hash, "error", err)
			continue
		}
		slog.InfoContext(ctx, "adding dismissed recipe to params", "title", recipe.Title, "hash", hash)
		p.Dismissed = append(p.Dismissed, *recipe)
	}

	hash := p.Hash()
	if list, err := s.FromCache(ctx, hash); err == nil {
		// TODO check not found error explicitly
		if r.URL.Query().Get("mail") == "true" {
			FormatMail(p, *list, w)
			return
		}
		FormatChatHTML(p, *list, w)
		//backfill
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

	// should this be in hash?
	p.ConversationID = strings.TrimSpace(r.URL.Query().Get("conversation_id"))

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
			CreatedAt: time.Now(),
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

// loadParamsFromHash loads generator params from cache using the hash
func (s *server) loadParamsFromHash(ctx context.Context, hash string) (*generatorParams, error) {
	paramsReader, err := s.cache.Get(ctx, hash+".params")
	if err != nil {
		return nil, fmt.Errorf("params not found for hash %s: %w", hash, err)
	}
	defer paramsReader.Close()

	var params generatorParams
	if err := json.NewDecoder(paramsReader).Decode(&params); err != nil {
		return nil, fmt.Errorf("failed to decode params: %w", err)
	}
	return &params, nil
}
