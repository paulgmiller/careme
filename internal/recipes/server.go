package recipes

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/users"
)

type server struct {
	cfg           *config.Config
	storage       *users.Storage
	generator     *Generator
	clarityScript template.HTML
	spinnerTmpl   *template.Template
	mux           *http.ServeMux
}

// NewHandler returns an http.Handler serving the recipe endpoints under /recipes.
func NewHandler(cfg *config.Config, storage *users.Storage, generator *Generator, clarityScript template.HTML, spinnerTmpl *template.Template) http.Handler {
	s := &server{
		cfg:           cfg,
		storage:       storage,
		generator:     generator,
		clarityScript: clarityScript,
		spinnerTmpl:   spinnerTmpl,
	}
	router := http.NewServeMux()
	router.HandleFunc("/", s.handleRecipes)
	s.mux = router
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/recipes")
	if path == "" {
		path = "/"
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = path
	s.mux.ServeHTTP(w, r2)
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

	l, err := locations.GetLocationByID(ctx, s.cfg, loc)
	if err != nil {
		http.Error(w, "could not get location details", http.StatusBadRequest)
		return
	}

	p := DefaultParams(l, date)
	for _, last := range currentUser.LastRecipes {
		if last.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
			continue
		}
		p.LastRecipes = append(p.LastRecipes, last.Title)
	}

	if instructions := r.URL.Query().Get("instructions"); instructions != "" {
		p.Instructions = instructions
	}

	p.UserID = currentUser.ID

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
