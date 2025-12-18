package users

import (
	"careme/internal/locations"
	"careme/internal/templates"
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
	storage       *Storage
	clarityScript template.HTML
	userTmpl      *template.Template //just remove or is htis useful?
	locGetter     locationGetter
}

// NewHandler returns an http.Handler that serves the user related routes under /user.
func NewHandler(storage *Storage, clarityScript template.HTML, locGetter locationGetter) *server {
	return &server{
		storage:       storage,
		clarityScript: clarityScript,
		userTmpl:      templates.User,
		locGetter:     locGetter,
	}
}

func (s *server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/user", s.handleUser)
	mux.HandleFunc("POST /user/recipes", s.handleUserRecipes)
}

func (s *server) handleUserRecipes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser, err := FromRequest(r, s.storage)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load user for user page", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	if currentUser == nil {
		http.Redirect(w, r, "/user", http.StatusSeeOther)
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
			http.Redirect(w, r, "/user", http.StatusSeeOther)
			return
		}
	}

	newRecipe := Recipe{
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
	http.Redirect(w, r, "/user", http.StatusSeeOther)
}

func (s *server) handleUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()

	// Check if viewing another user's profile via ID query parameter
	userID := r.URL.Query().Get("id")
	var viewUser *User
	var err error

	if userID != "" {
		// Admin viewing another user
		viewUser, err = s.storage.GetByID(userID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "user not found", http.StatusNotFound)
				return
			}
			slog.ErrorContext(ctx, "failed to load user by ID", "error", err, "user_id", userID)
			http.Error(w, "unable to load user", http.StatusInternalServerError)
			return
		}
	} else {
		// User viewing their own profile
		viewUser, err = FromRequest(r, s.storage)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				ClearCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			slog.ErrorContext(ctx, "failed to load user for user page", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		if viewUser == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	success := false
	// Only allow POST updates for own profile (no ID parameter)
	if r.Method == http.MethodPost && userID == "" {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form submission", http.StatusBadRequest)
			return
		}

		// Only update favorite_store if provided
		if favoriteStore := strings.TrimSpace(r.FormValue("favorite_store")); favoriteStore != "" || r.Form.Has("favorite_store") {
			viewUser.FavoriteStore = favoriteStore
		}

		// Only update shopping_day if provided
		if shoppingDay := strings.TrimSpace(r.FormValue("shopping_day")); shoppingDay != "" {
			viewUser.ShoppingDay = shoppingDay
		}

		if err := s.storage.Update(viewUser); err != nil {
			slog.ErrorContext(ctx, "failed to update user", "error", err)
			http.Error(w, "unable to save preferences", http.StatusInternalServerError)
			return
		}
		success = true
	}

	// Fetch location name if favorite store is set
	var favoriteStoreName string
	if viewUser.FavoriteStore != "" && s.locGetter != nil {
		loc, err := s.locGetter.GetLocationByID(ctx, viewUser.FavoriteStore)
		if err != nil {
			slog.WarnContext(ctx, "failed to get location name for favorite store", "location_id", viewUser.FavoriteStore, "error", err)
			favoriteStoreName = viewUser.FavoriteStore // fallback to ID
		} else {
			favoriteStoreName = loc.Name
		}
	}

	data := struct {
		ClarityScript     template.HTML
		User              *User
		Success           bool
		FavoriteStoreName string
	}{
		ClarityScript:     s.clarityScript,
		User:              viewUser,
		Success:           success,
		FavoriteStoreName: favoriteStoreName,
	}
	if err := s.userTmpl.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "user template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
