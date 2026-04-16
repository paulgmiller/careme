package critique

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"sort"

	"careme/internal/ai"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

type adminCritiqueView struct {
	RecipeTitle string
	RecipeURL   string
	ai.RecipeCritique
}

var adminCritiquesPageTmpl = template.Must(template.New("admin-critiques").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Admin Recipe Critiques</title>
</head>
<body>
  <nav>
    <a href="/admin/users">Users</a> |
    <a href="/admin/critiques">Recipe Critiques</a>
  </nav>
  <h1>Recipe Critiques</h1>
  <p>Total critiques: {{len .Critiques}}</p>
  <table border="1" cellpadding="6" cellspacing="0">
    <thead>
      <tr>
        <th>Recipe</th>
        <th>Score</th>
        <th>Summary</th>
        <th>Details</th>
      </tr>
    </thead>
    <tbody>
      {{range .Critiques}}
      <tr>
        <td>
          <a href="{{.RecipeURL}}">{{.RecipeTitle}}</a>
        </td>
        <td>{{.OverallScore}}/10</td>
        <td>
          {{.Summary}}
        </td>
        <td>
          <details>
            <summary>Open critique</summary>
            {{if .Strengths}}
            <p><strong>Strengths</strong></p>
            <ul>
              {{range .Strengths}}
              <li>{{.}}</li>
              {{end}}
            </ul>
            {{end}}
            {{if .Issues}}
            <p><strong>Issues</strong></p>
            <ul>
              {{range .Issues}}
              <li>{{.Severity}} / {{.Category}}: {{.Detail}}</li>
              {{end}}
            </ul>
            {{end}}
            {{if .SuggestedFixes}}
            <p><strong>Suggested fixes</strong></p>
            <ul>
              {{range .SuggestedFixes}}
              <li>{{.}}</li>
              {{end}}
            </ul>
            {{end}}
          </details>
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>`))

type recipeio interface {
	SingleFromCache(ctx context.Context, hash string) (*ai.Recipe, error)
}

func AdminCritiquesPage(s store, rio recipeio) http.Handler {
	if rio == nil {
		panic("store and recipeio must not be nil")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		hashes, err := s.ListHashes(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to list recipe critiques for admin page", "error", err)
			http.Error(w, "unable to load recipe critiques", http.StatusInternalServerError)
			return
		}

		views, err := loadAdminCritiqueViews(r.Context(), s, rio, hashes)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to load recipe critiques for admin page", "error", err)
			http.Error(w, "unable to load recipe critiques", http.StatusInternalServerError)
			return
		}
		views = lo.Compact(views)
		sort.Slice(views, func(i, j int) bool {
			return views[i].CritiquedAt.After(views[j].CritiquedAt)
		})

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := adminCritiquesPageTmpl.Execute(w, struct {
			Critiques []*adminCritiqueView
		}{Critiques: views}); err != nil {
			slog.ErrorContext(r.Context(), "failed to render admin recipe critiques page", "error", err)
			http.Error(w, "unable to render recipe critiques", http.StatusInternalServerError)
			return
		}
	})
}

func loadAdminCritiqueViews(
	ctx context.Context,
	store store,
	rio recipeio,
	hashes []string,
) ([]*adminCritiqueView, error) {
	views, err := parallelism.MapWithErrors(hashes, func(hash string) (*adminCritiqueView, error) {
		view := adminCritiqueView{
			RecipeURL: "/recipe/" + hash,
		}

		cachedCritique, err := store.Load(ctx, hash)
		if err != nil {
			return nil, err
		}
		view.RecipeCritique = *cachedCritique

		recipeTitle, err := rio.SingleFromCache(ctx, hash)
		if err != nil {
			slog.InfoContext(ctx, "failed to load recipe for admin critiques page", "hash", hash, "error", err)
			view.RecipeTitle = "Unknown recipe"
		} else {
			view.RecipeTitle = recipeTitle.Title
		}

		return &view, nil
	})
	return views, err
}
