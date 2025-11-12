package users

import (
	"careme/internal/templates"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type server struct {
	storage       *Storage
	clarityScript template.HTML
	userTmpl      *template.Template //just remove or is htis useful?
}

// NewHandler returns an http.Handler that serves the user related routes under /user.
func NewHandler(storage *Storage, clarityScript template.HTML) *server {
	return &server{
		storage:       storage,
		clarityScript: clarityScript,
		userTmpl:      templates.User,
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
	currentUser, err := FromRequest(r, s.storage)
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
	if currentUser == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	success := false
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form submission", http.StatusBadRequest)
			return
		}
		currentUser.FavoriteStore = strings.TrimSpace(r.FormValue("favorite_store"))
		currentUser.ShoppingDay = strings.TrimSpace(r.FormValue("shopping_day"))

		if err := s.storage.Update(currentUser); err != nil {
			slog.ErrorContext(ctx, "failed to update user", "error", err)
			http.Error(w, "unable to save preferences", http.StatusInternalServerError)
			return
		}
		success = true
	}

	data := struct {
		ClarityScript template.HTML
		User          *User
		Success       bool
	}{
		ClarityScript: s.clarityScript,
		User:          currentUser,
		Success:       success,
	}
	if err := s.userTmpl.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "user template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
