package gradereview

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
)

type Server struct {
	store   *Store
	catalog Catalog
	now     func() time.Time
}

type Catalog interface {
	LoadCatalog(context.Context, string) (*locations.Location, []ai.InputIngredient, error)
}

type Options struct{ Catalog Catalog }

func NewHandler(c cache.ListCache, cacheVersion string, options Options) http.Handler {
	return handler(&Server{store: NewStore(c, cacheVersion), catalog: options.Catalog, now: time.Now})
}

func newHandler(store *Store) http.Handler {
	server := &Server{
		store: store,
		now:   time.Now,
	}
	return handler(server)
}

func handler(server *Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /grader", server.handleIndex)
	mux.HandleFunc("GET /grader/", server.handleIndex)
	mux.HandleFunc("POST /grader/review", server.handleReview)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	locationID := strings.TrimSpace(r.URL.Query().Get("location"))
	var candidate *Candidate
	var location *locations.Location
	var err error
	if locationID == "" {
		candidate, err = s.store.Next(r.Context())
	} else {
		var ingredients []ai.InputIngredient
		location, ingredients, err = s.loadCatalog(r.Context(), locationID)
		if err == nil {
			candidate, err = s.store.NextFromCatalog(r.Context(), ingredients)
		}
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to load ingredient grade for review", "error", err)
		http.Error(w, "Could not load ingredient grades.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, struct {
		*Candidate
		Location *locations.Location
	}{candidate, location}); err != nil {
		slog.ErrorContext(r.Context(), "failed to render ingredient grade review", "error", err)
	}
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid review.", http.StatusBadRequest)
		return
	}

	gradeKey := strings.TrimSpace(r.PostFormValue("grade_key"))
	verdict := Verdict(r.PostFormValue("verdict"))
	if gradeKey == "" || !verdict.Valid() {
		http.Error(w, "Choose too high, correct, or too low.", http.StatusBadRequest)
		return
	}

	locationID := strings.TrimSpace(r.PostFormValue("location"))
	var err error
	if locationID == "" {
		err = s.store.Save(r.Context(), gradeKey, verdict, s.now())
	} else {
		var ingredients []ai.InputIngredient
		_, ingredients, err = s.loadCatalog(r.Context(), locationID)
		if err == nil {
			err = s.store.SaveFromCatalog(r.Context(), locationID, gradeKey, ingredients, verdict, s.now())
		}
	}
	switch {
	case err == nil, errors.Is(err, cache.ErrAlreadyExists):
		target := "/grader"
		if locationID != "" {
			target += "?" + url.Values{"location": {locationID}}.Encode()
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	case errors.Is(err, cache.ErrNotFound):
		http.Error(w, "Ingredient grade not found.", http.StatusNotFound)
	case errors.Is(err, ErrInvalidVerdict):
		http.Error(w, "Choose too high, correct, or too low.", http.StatusBadRequest)
	default:
		slog.ErrorContext(r.Context(), "failed to save ingredient grade review", "error", err)
		http.Error(w, "Could not save the review.", http.StatusInternalServerError)
	}
}

func (s *Server) loadCatalog(ctx context.Context, locationID string) (*locations.Location, []ai.InputIngredient, error) {
	if s.catalog == nil {
		return nil, nil, errors.New("store catalog is not configured")
	}
	return s.catalog.LoadCatalog(ctx, locationID)
}

var pageTemplate = template.Must(template.New("ingredient-grade-review").Funcs(template.FuncMap{
	"join": strings.Join,
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Ingredient grade review</title>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, sans-serif; background: #f7f3ea; color: #29251f; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 24px; }
    main { width: min(100%, 680px); }
    header { margin-bottom: 18px; }
    h1 { margin: 0; font-family: Georgia, serif; font-size: clamp(1.65rem, 5vw, 2.35rem); line-height: 1; }
    .card { background: #fffdf8; border: 1px solid #ded6c8; border-radius: 18px; padding: clamp(24px, 6vw, 42px); box-shadow: 0 16px 50px rgb(70 53 25 / 10%); }
    .eyebrow { margin: 0 0 10px; color: #796f62; font-size: .8rem; font-weight: 750; letter-spacing: .08em; text-transform: uppercase; }
    h2 { margin: 0; font-family: Georgia, serif; font-size: clamp(1.7rem, 6vw, 2.7rem); line-height: 1.08; }
    .details { margin: 10px 0 28px; color: #655e54; }
    .grade { display: grid; grid-template-columns: auto 1fr; gap: 18px; align-items: center; background: #f5f0e5; border-radius: 14px; padding: 18px; }
    .score { width: 72px; height: 72px; display: grid; place-items: center; border-radius: 50%; background: #244c3a; color: white; font-size: 1.45rem; font-weight: 800; }
    .score small { font-size: .7rem; font-weight: 500; opacity: .75; }
    .reason { margin: 0; line-height: 1.45; }
    .question { margin: 28px 0 14px; font-weight: 750; }
    .actions { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; }
    button { min-height: 52px; border: 0; border-radius: 10px; padding: 12px; color: white; font: inherit; font-weight: 750; cursor: pointer; }
    button:hover { filter: brightness(.94); }
    button:focus-visible { outline: 3px solid #29251f; outline-offset: 3px; }
    .high { background: #a34436; }
    .correct { background: #2e684e; }
    .low { background: #396994; }
    .done { text-align: center; padding: 28px 0; }
    .done p { color: #655e54; }
    @media (max-width: 520px) { header { align-items: start; flex-direction: column; gap: 8px; } .actions { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
  <main>
    <header>
      <h1>Ingredient grade check</h1>
      {{if .Location}}<p>{{.Location.Name}} · {{.Location.ID}}</p>{{end}}
    </header>
    <section class="card">
      {{if .Ingredient.Grade}}
        <p class="eyebrow">{{if .Ingredient.Brand}}{{.Ingredient.Brand}}{{else}}Ingredient{{end}}</p>
        <h2>{{if .Ingredient.Description}}{{.Ingredient.Description}}{{else}}{{.Ingredient.ProductID}}{{end}}</h2>
        <p class="details">{{.Ingredient.Size}}{{if .Ingredient.Categories}}{{if .Ingredient.Size}} · {{end}}{{join .Ingredient.Categories ", "}}{{end}}</p>
        <div class="grade">
          <div class="score">{{.Ingredient.Grade.Score}}<small>/10</small></div>
          <p class="reason">{{.Ingredient.Grade.Reason}}</p>
        </div>
        <p class="question">How does this grade look?</p>
        <form method="post" action="/grader/review">
          {{if .Location}}<input type="hidden" name="location" value="{{.Location.ID}}">{{end}}
          <input type="hidden" name="grade_key" value="{{.GradeKey}}">
          <div class="actions">
            <button class="high" type="submit" name="verdict" value="too_high">Too high</button>
            <button class="correct" type="submit" name="verdict" value="correct">Correct</button>
            <button class="low" type="submit" name="verdict" value="too_low">Too low</button>
          </div>
        </form>
      {{else}}
        <div class="done">
          {{if .Location}}<h2>All grades reviewed, chef</h2><p>You’ve reviewed every ingredient in this store’s catalog.</p>{{else}}<h2>No grades found</h2><p>Refresh to try another batch.</p>{{end}}
        </div>
      {{end}}
    </section>
  </main>
</body>
</html>`))
