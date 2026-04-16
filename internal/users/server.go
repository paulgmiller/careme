package users

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes/feedback"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"

	utypes "careme/internal/users/types"

	"github.com/samber/lo"
)

type locationGetter interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type server struct {
	storage   *Storage
	userTmpl  *template.Template // just remove or is this useful?
	locGetter locationGetter
	clerk     auth.AuthClient // make an interface
}

type pastRecipeView struct {
	utypes.Recipe
	Cooked           bool
	CookedStars      string
	CookedStarsLabel string
}

const (
	cookedPastRecipesWindow = 28 * 24 * time.Hour
	savedPastRecipesWindow  = 14 * 24 * time.Hour
)

// NewHandler returns an http.Handler that serves the user related routes under /user.
func NewHandler(storage *Storage, locGetter locationGetter, clerkClient auth.AuthClient) *server {
	return &server{
		storage:   storage,
		userTmpl:  templates.User,
		locGetter: locGetter,
		clerk:     clerkClient,
	}
}

func (s *server) Register(mux routing.Registrar) {
	mux.HandleFunc("/user", s.handleUser)
	mux.HandleFunc("POST /user/recipes/remove", s.handleRemoveUserRecipe)
	mux.HandleFunc("POST /user/favorite", s.handleFavorite)
	mux.HandleFunc("GET /user/exists", s.handleExists)
}

func (s *server) handleExists(w http.ResponseWriter, r *http.Request) {
	clerkUserID, err := s.clerk.GetUserIDFromRequest(r)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			http.Error(w, "no valid session found", http.StatusUnauthorized)
			return
		}
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	exists, err := s.exists(clerkUserID)
	if err != nil {
		slog.ErrorContext(r.Context(), "auth user exists lookup failed", "clerk_user_id", clerkUserID, "error", err)
		http.Error(w, "unable to check account", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(struct {
		Exists bool `json:"exists"`
	}{
		Exists: exists,
	}); err != nil {
		slog.ErrorContext(r.Context(), "auth user exists encode failed", "clerk_user_id", clerkUserID, "error", err)
	}
}

func (s *server) exists(uid string) (bool, error) {
	_, err := s.storage.GetByID(uid)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *server) handleRemoveUserRecipe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if !errors.Is(err, auth.ErrNoSession) {
			slog.ErrorContext(ctx, "failed to get clerk user ID", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	recipeTitle := strings.TrimSpace(r.FormValue("recipe"))
	recipeHash := strings.TrimSpace(r.FormValue("hash"))
	recipeCreatedAtRaw := strings.TrimSpace(r.FormValue("created_at"))
	if recipeTitle == "" || recipeCreatedAtRaw == "" {
		http.Error(w, "invalid recipe selection", http.StatusBadRequest)
		return
	}

	recipeCreatedAt, err := time.Parse(time.RFC3339Nano, recipeCreatedAtRaw)
	if err != nil {
		http.Error(w, "invalid recipe selection", http.StatusBadRequest)
		return
	}

	removed, err := s.storage.RemoveRecipe(currentUser, utypes.Recipe{
		Title:     recipeTitle,
		Hash:      recipeHash,
		CreatedAt: recipeCreatedAt,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to update user when removing recipe", "error", err)
		http.Error(w, "unable to save preferences", http.StatusInternalServerError)
		return
	}
	if !removed {
		http.Redirect(w, r, "/user?tab=past", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/user?tab=past", http.StatusSeeOther)
}

func (s *server) handleUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	ctx := r.Context()
	activeTab := "customize"
	if r.URL.Query().Get("tab") == "past" {
		activeTab = "past"
	}
	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if !errors.Is(err, auth.ErrNoSession) {
			slog.ErrorContext(ctx, "failed to get clerk user ID", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		// if session expires this is less than optimal. We want to give them just the
		// clerk_refresh and seee if they are then logged in. But we only want to do that once?
		// TODO stick just show a sign in button on user page if no session
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	success := false
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form submission", http.StatusBadRequest)
			return
		}

		// Only update favorite_store if provided
		if favoriteStore := strings.TrimSpace(r.FormValue("favorite_store")); favoriteStore != "" || r.Form.Has("favorite_store") {
			currentUser.FavoriteStore = favoriteStore
		}

		// Only update shopping_day if provided
		if shoppingDay := strings.TrimSpace(r.FormValue("shopping_day")); shoppingDay != "" {
			currentUser.ShoppingDay = shoppingDay
		}
		if r.Form.Has("directive") {
			generationPrompt := strings.TrimSpace(r.FormValue("directive"))
			currentUser.Directive = generationPrompt
		}
		currentUser.MailOptIn = r.FormValue("mail_opt_in") == "1"

		if err := s.storage.Update(currentUser); err != nil {
			slog.ErrorContext(ctx, "failed to update user", "error", err)
			http.Error(w, "unable to save preferences", http.StatusInternalServerError)
			return
		}
		success = true
		activeTab = "customize"
	}

	userCopy := *currentUser
	userForTemplate := &userCopy

	// Fetch location name if favorite store is set
	var favoriteStoreName string
	if userForTemplate.FavoriteStore != "" && s.locGetter != nil {
		loc, err := s.locGetter.GetLocationByID(ctx, userForTemplate.FavoriteStore)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get location name for favorite store", "location_id", userForTemplate.FavoriteStore, "error", err)
			userForTemplate.FavoriteStore = ""
		} else {
			favoriteStoreName = loc.Name
		}
	}

	data := struct {
		ClarityScript     template.HTML
		GoogleTagScript   template.HTML
		User              *utypes.User
		Success           bool
		FavoriteStoreName string
		ActiveTab         string
		PastRecipes       []pastRecipeView
		Style             seasons.Style
		ServerSignedIn    bool
	}{
		ClarityScript:     templates.ClarityScript(ctx),
		GoogleTagScript:   templates.GoogleTagScript(),
		User:              userForTemplate,
		Success:           success,
		FavoriteStoreName: favoriteStoreName,
		ActiveTab:         activeTab,
		PastRecipes:       pastRecipeViews(ctx, s.storage.cache, userForTemplate.LastRecipes),
		Style:             seasons.GetCurrentStyle(),
		ServerSignedIn:    true,
	}
	if err := s.userTmpl.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "user template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func pastRecipeViews(ctx context.Context, c cache.Cache, recipes []utypes.Recipe) []pastRecipeView {
	now := time.Now()
	cookedCutoff := now.Add(-cookedPastRecipesWindow)
	savedCutoff := now.Add(-savedPastRecipesWindow)
	// need a more efficient way to do this. Might be pagination/db time
	recentRecipes := lo.Filter(recipes, func(recipe utypes.Recipe, _ int) bool {
		if recipe.Hash == "" {
			return false // come back and deal with manual later
		}
		return recipe.CreatedAt.After(cookedCutoff)
	})

	feedbackIO := feedback.NewIO(c)
	hashes := make([]string, 0, len(recentRecipes))
	for _, recipe := range recentRecipes {
		hashes = append(hashes, recipe.Hash)
	}
	feedbackByHash := feedbackIO.FeedbackByHash(ctx, hashes)

	return lo.FilterMap(recentRecipes, func(recipe utypes.Recipe, _ int) (pastRecipeView, bool) {
		state, ok := feedbackByHash[recipe.Hash]
		cooked := ok && state.Cooked
		if !cooked && recipe.CreatedAt.Before(savedCutoff) {
			return pastRecipeView{}, false
		}
		return pastRecipeView{
			Recipe:           recipe,
			Cooked:           cooked,
			CookedStars:      cookedStars(ok, state),
			CookedStarsLabel: cookedStarsLabel(ok, state),
		}, true
	})
}

func cookedStars(ok bool, state feedback.Feedback) string {
	if !ok || !state.Cooked {
		return ""
	}
	stars := state.Stars
	if stars < 1 {
		return "🔪"
	}
	return strings.Repeat("⭐", stars)
}

func cookedStarsLabel(ok bool, state feedback.Feedback) string {
	if !ok || !state.Cooked {
		return ""
	}
	stars := state.Stars
	if stars < 1 {
		return "Cooked"
	}
	if stars == 1 {
		return "Rated 1 star"
	}
	return "Rated " + strconv.Itoa(stars) + " stars"
}

func (s *server) handleFavorite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !isHTMXRequest(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if !errors.Is(err, auth.ErrNoSession) {
			slog.ErrorContext(ctx, "failed to get clerk user ID", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		w.Header().Set("HX-Redirect", "/")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form submission", http.StatusBadRequest)
		return
	}

	favoriteStore := strings.TrimSpace(r.FormValue("favorite_store"))
	if favoriteStore == "" && !r.Form.Has("favorite_store") {
		http.Error(w, "missing favorite_store", http.StatusBadRequest)
		return
	}
	currentUser.FavoriteStore = favoriteStore
	if err := s.storage.Update(currentUser); err != nil {
		slog.ErrorContext(ctx, "failed to update user", "error", err)
		http.Error(w, "unable to save preferences", http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}

func isHTMXRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}
