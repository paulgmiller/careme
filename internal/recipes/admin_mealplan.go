package recipes

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
)

type adminMealPlanPageData struct {
	StartHash      string
	Lists          []adminMealPlanListView
	Warnings       []string
	TotalPlanCount int
}

type adminMealPlanListView struct {
	Hash          string
	RecipesURL    string
	ParamsURL     string
	MenuPromptURL string
	Store         string
	Date          string
	Instructions  string
	Plans         []ai.RecipePlan
}

var adminMealPlanPageTmpl = template.Must(template.New("admin-mealplan").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Admin Meal Plan</title>
</head>
<body>
  <nav>
    <a href="/admin/users">Users</a> |
    <a href="/admin/critiques">Recipe Critiques</a> |
    <a href="/admin/mealplan/{{.StartHash}}">Meal Plan</a>
  </nav>
  <h1>Meal Plan Chain</h1>
  <p>Starting hash: <code>{{.StartHash}}</code></p>
  <p>Shopping lists visited: {{len .Lists}}</p>
  <p>Total plan entries: {{.TotalPlanCount}}</p>

  {{if .Warnings}}
  <h2>Warnings</h2>
  <ul>
    {{range .Warnings}}
    <li>{{.}}</li>
    {{end}}
  </ul>
  {{end}}

  {{range .Lists}}
  <section>
    <h2><code>{{.Hash}}</code></h2>
    <p>
      {{if .Store}}{{.Store}}{{else}}Unknown store{{end}}
      {{if .Date}} on {{.Date}}{{end}}
    </p>
    {{if .Instructions}}
    <p><strong>Instructions:</strong> {{.Instructions}}</p>
    {{end}}
    <p>
      <a href="{{.RecipesURL}}">Shopping list</a> |
      <a href="{{.ParamsURL}}">Params JSON</a> |
      <a href="{{.MenuPromptURL}}">Menu prompt JSON</a>
    </p>
    {{if .Plans}}
    <table border="1" cellpadding="6" cellspacing="0">
      <thead>
        <tr>
          <th>Cuisine</th>
          <th>Anchor</th>
          <th>Technique</th>
          <th>Side</th>
          <th>Fancy</th>
          <th>Recipe Instructions</th>
        </tr>
      </thead>
      <tbody>
        {{range .Plans}}
        <tr>
          <td>{{.Cuisine}}</td>
          <td>{{.AnchorIngredient}}</td>
          <td>{{.Technique}}</td>
          <td>{{.SideVegetable}}</td>
          <td>{{.Fancy}}</td>
          <td>
            {{if .RecipeInstructions}}
            <ul>
              {{range .RecipeInstructions}}
              <li>{{.}}</li>
              {{end}}
            </ul>
            {{else}}
            none
            {{end}}
          </td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
    <p>No meal plan entries stored for this shopping list.</p>
    {{end}}
  </section>
  {{end}}
</body>
</html>`))

// AdminMealPlanPage renders the menu-plan chain reachable from a shopping-list hash.
func AdminMealPlanPage(rio recipeio) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		hash := strings.TrimSpace(r.PathValue("hash"))
		if hash == "" {
			http.Error(w, "missing shopping list hash", http.StatusBadRequest)
			return
		}

		data, err := loadAdminMealPlanPageData(r.Context(), rio, hash)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				http.Error(w, "meal plan not found", http.StatusNotFound)
				return
			}
			slog.ErrorContext(r.Context(), "failed to load admin meal plan page", "hash", hash, "error", err)
			http.Error(w, "unable to load meal plan", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := adminMealPlanPageTmpl.Execute(w, data); err != nil {
			slog.ErrorContext(r.Context(), "failed to render admin meal plan page", "hash", hash, "error", err)
			http.Error(w, "unable to render meal plan", http.StatusInternalServerError)
			return
		}
	})
}

func loadAdminMealPlanPageData(ctx context.Context, rio recipeio, startHash string) (adminMealPlanPageData, error) {
	data := adminMealPlanPageData{StartHash: startHash}
	queue := []string{startHash}
	visited := make(map[string]struct{})

	for len(queue) > 0 {
		hash := queue[0]
		queue = queue[1:]
		if _, ok := visited[hash]; ok {
			continue
		}
		visited[hash] = struct{}{}

		list, err := rio.FromCache(ctx, hash)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) && hash != startHash {
				data.Warnings = append(data.Warnings, "shopping list not found: "+hash)
				continue
			}
			return data, err
		}

		params, err := rio.ParamsFromCache(ctx, hash)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) && hash != startHash {
				data.Warnings = append(data.Warnings, "params not found: "+hash)
				continue
			}
			return data, err
		}

		view := adminMealPlanListView{
			Hash:          hash,
			RecipesURL:    "/recipes?h=" + hash,
			ParamsURL:     "/admin/params/" + hash,
			MenuPromptURL: "/admin/prompt/menu/" + hash,
			Date:          params.Date.Format("2006-01-02"),
			Instructions:  strings.TrimSpace(params.Instructions),
		}
		if params.Location != nil {
			view.Store = params.Location.Name
			if strings.TrimSpace(view.Store) == "" {
				view.Store = params.Location.ID
			}
		}
		if list.Plan != nil {
			view.Plans = append([]ai.RecipePlan(nil), list.Plan.Plans...)
			data.TotalPlanCount += len(view.Plans)
		}
		data.Lists = append(data.Lists, view)

		for _, recipe := range params.Saved {
			originHash := normalizeAdminMealPlanOriginHash(recipe.OriginHash)
			if originHash == "" || originHash == hash {
				continue
			}
			if _, ok := visited[originHash]; ok {
				continue
			}
			queue = append(queue, originHash)
		}
	}

	return data, nil
}

func normalizeAdminMealPlanOriginHash(hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return ""
	}
	if normalizedHash, ok := legacyHashToCurrent(hash, legacyRecipeHashSeed); ok {
		return normalizedHash
	}
	return hash
}
