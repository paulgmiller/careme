package recipes

import (
	"context"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"time"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/templates"
	"careme/internal/users"
)

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type server struct {
	cfg           *config.Config
	storage       *users.Storage
	generator     *Generator
	clarityScript template.HTML
	spinnerTmpl   *template.Template //remove?
	locServer     locServer
}

// NewHandler returns an http.Handler serving the recipe endpoints under /recipes.
func NewHandler(cfg *config.Config, storage *users.Storage, generator *Generator, clarityScript template.HTML, locServer locServer) *server {
	return &server{
		cfg:           cfg,
		storage:       storage,
		generator:     generator,
		clarityScript: clarityScript,
		spinnerTmpl:   templates.Spin,
		locServer:     locServer,
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

	recipe, err := s.generator.SingleFromCache(ctx, hash)
	if err != nil {
		http.Error(w, "recipe not found", http.StatusNotFound)
		return
	}

	p := DefaultParams(&locations.Location{
		ID:   "",
		Name: "Unknown Location",
	}, time.Now())

	list := ai.ShoppingList{
		Recipes: []ai.Recipe{*recipe},
	}

	slog.InfoContext(ctx, "serving shared recipe by hash", "hash", hash)
	if err := s.generator.FormatChatHTML(p, list, w); err != nil {
		http.Error(w, "failed to format recipe", http.StatusInternalServerError)
	}
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
		if err := s.generator.FromCache(ctx, hashParam, nil, w); err != nil {
			slog.ErrorContext(ctx, "failed to load shared recipe for hash", "hash", hashParam, "error", err)
			http.Error(w, "recipe not found or expired", http.StatusNotFound)
		}
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
		if ingredientblob, err := s.generator.cache.Get(lochash); err == nil {
			slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash)
			defer ingredientblob.Close()
			io.Copy(w, ingredientblob)
			w.Header().Add("Content-Type", "application/json")
		}
	}

	for _, last := range currentUser.LastRecipes {
		if last.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
			continue
		}
		p.LastRecipes = append(p.LastRecipes, last.Title)
	}

	if instructions := r.URL.Query().Get("instructions"); instructions != "" {
		p.Instructions = instructions
	}

	hash := p.Hash()
	if err := s.generator.FromCache(ctx, hash, p, w); err == nil {
		return
	}

	go func() {
		slog.InfoContext(ctx, "generating cached recipes", "params", p.String(), "hash", hash)
		if err := s.generator.GenerateRecipes(ctx, p); err != nil {
			slog.ErrorContext(ctx, "generate error", "error", err)
		}
	}()

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	spinnerData := struct {
		ClarityScript template.HTML
	}{
		ClarityScript: s.clarityScript,
	}
	if err := s.spinnerTmpl.Execute(w, spinnerData); err != nil {
		slog.ErrorContext(ctx, "home template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
