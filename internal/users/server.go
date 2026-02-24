package users

import (
	"careme/internal/auth"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"
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

// NewHandler returns an http.Handler that serves the user related routes under /user.
func NewHandler(storage *Storage, locGetter locationGetter, clerkClient auth.AuthClient) *server {
	return &server{
		storage:   storage,
		userTmpl:  templates.User,
		locGetter: locGetter,
		clerk:     clerkClient,
	}
}

func (s *server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/user", s.handleUser)
	mux.HandleFunc("POST /user/recipes", s.handleUserRecipes)
	mux.HandleFunc("POST /user/favorite", s.handleFavorite)
}

// used on user page to manaully save recipes
func (s *server) handleUserRecipes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk) // just for logging purposes in kickgeneration. We could do this in the generateion function instead to avoid the extra call on every not found.
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
	if recipeTitle == "" {
		slog.ErrorContext(ctx, "no recipe title provided")
		http.Error(w, "no recipe title provided", http.StatusBadRequest)
		return
	}

	hash := strings.TrimSpace(r.FormValue("hash"))

	for _, existing := range currentUser.LastRecipes {
		if strings.EqualFold(existing.Title, recipeTitle) {
			slog.InfoContext(ctx, "duplicate previous recipe", "title", recipeTitle)
			http.Redirect(w, r, "/user?tab=past", http.StatusSeeOther)
			return
		}
	}

	newRecipe := utypes.Recipe{
		Title:     recipeTitle,
		Hash:      hash,
		CreatedAt: time.Now(),
	}
	currentUser.LastRecipes = append(currentUser.LastRecipes, newRecipe)
	if err := s.storage.Update(currentUser); err != nil {
		slog.ErrorContext(ctx, "failed to update user", "error", err)
		http.Error(w, "unable to save preferences", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/user?tab=past", http.StatusSeeOther)
}

func (s *server) handleUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	activeTab := "customize"
	if r.URL.Query().Get("tab") == "past" {
		activeTab = "past"
	}
	clerkUserID, err := s.clerk.GetUserIDFromRequest(r)
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

	currentUser, err := s.storage.FindOrCreateFromClerk(ctx, clerkUserID, s.clerk)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get user by clerk ID", "clerk_user_id", clerkUserID, "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
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

	// Fetch location name if favorite store is set
	var favoriteStoreName string
	if currentUser.FavoriteStore != "" && s.locGetter != nil {
		loc, err := s.locGetter.GetLocationByID(ctx, currentUser.FavoriteStore)
		if err != nil {
			slog.WarnContext(ctx, "failed to get location name for favorite store", "location_id", currentUser.FavoriteStore, "error", err)
			favoriteStoreName = currentUser.FavoriteStore // fallback to ID
		} else {
			favoriteStoreName = loc.Name
		}
	}
	// TODO paginate and search on page instead.
	if len(currentUser.LastRecipes) > 14 {
		currentUser.LastRecipes = currentUser.LastRecipes[0:14]
	}
	data := struct {
		ClarityScript     template.HTML
		User              *utypes.User
		Success           bool
		FavoriteStoreName string
		ActiveTab         string
		Style             seasons.Style
		ServerSignedIn    bool
	}{
		ClarityScript:     templates.ClarityScript(),
		User:              currentUser,
		Success:           success,
		FavoriteStoreName: favoriteStoreName,
		ActiveTab:         activeTab,
		Style:             seasons.GetCurrentStyle(),
		ServerSignedIn:    true,
	}
	if err := s.userTmpl.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "user template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
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
	clerkUserID, err := s.clerk.GetUserIDFromRequest(r)
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

	currentUser, err := s.storage.FindOrCreateFromClerk(ctx, clerkUserID, s.clerk)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get user by clerk ID", "clerk_user_id", clerkUserID, "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
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
