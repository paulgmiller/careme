package gradereview

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"careme/internal/cache"
)

type Server struct {
	store *Store
	now   func() time.Time
}

func NewHandler(c cache.ListCache) http.Handler {
	server := &Server{
		store: NewStore(c),
		now:   time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", server.handleIndex)
	mux.HandleFunc("POST /review", server.handleReview)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	candidate, err := s.store.Next(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to load ingredient grade for review", "error", err)
		http.Error(w, "Could not load ingredient grades.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, candidate); err != nil {
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

	err := s.store.Save(r.Context(), gradeKey, verdict, s.now())
	switch {
	case err == nil, errors.Is(err, cache.ErrAlreadyExists):
		http.Redirect(w, r, "/", http.StatusSeeOther)
	case errors.Is(err, cache.ErrNotFound):
		http.Error(w, "Ingredient grade not found.", http.StatusNotFound)
	case errors.Is(err, ErrInvalidVerdict):
		http.Error(w, "Choose too high, correct, or too low.", http.StatusBadRequest)
	default:
		slog.ErrorContext(r.Context(), "failed to save ingredient grade review", "error", err)
		http.Error(w, "Could not save the review.", http.StatusInternalServerError)
	}
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
    header { display: flex; justify-content: space-between; gap: 24px; align-items: end; margin-bottom: 18px; }
    h1 { margin: 0; font-family: Georgia, serif; font-size: clamp(1.65rem, 5vw, 2.35rem); line-height: 1; }
    .progress { margin: 0; color: #6d6559; white-space: nowrap; }
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
      <p class="progress">{{.Reviewed}} of {{.Total}} reviewed</p>
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
        <form method="post" action="/review">
          <input type="hidden" name="grade_key" value="{{.GradeKey}}">
          <div class="actions">
            <button class="high" type="submit" name="verdict" value="too_high">Too high</button>
            <button class="correct" type="submit" name="verdict" value="correct">Correct</button>
            <button class="low" type="submit" name="verdict" value="too_low">Too low</button>
          </div>
        </form>
      {{else}}
        <div class="done">
          <h2>{{if .Total}}All caught up{{else}}No grades yet{{end}}</h2>
          <p>{{if .Total}}You reviewed every cached ingredient grade.{{else}}Run ingredient grading first, then refresh this page.{{end}}</p>
        </div>
      {{end}}
    </section>
  </main>
</body>
</html>`))
