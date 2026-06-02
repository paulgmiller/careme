package critique

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"sort"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/parallelism"
	"careme/internal/seasons"
	"careme/internal/static"

	"github.com/samber/lo"
)

type adminCritiqueView struct {
	RecipeTitle string
	RecipeURL   string
	ai.RecipeCritique
}

type CritiquePageTheme struct {
	Style             seasons.Style
	TailwindAssetPath string
}

func currentCritiquePageTheme() CritiquePageTheme {
	return CritiquePageTheme{
		Style:             seasons.GetCurrentStyle(),
		TailwindAssetPath: static.TailwindAssetPath,
	}
}

var adminCritiquesPageTmpl = template.Must(template.New("admin-critiques").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Admin Recipe Critiques | Careme</title>
  <style>
    :root{
      --brand-50:  {{.Style.Colors.C50}};
      --brand-100: {{.Style.Colors.C100}};
      --brand-200: {{.Style.Colors.C200}};
      --brand-300: {{.Style.Colors.C300}};
      --brand-400: {{.Style.Colors.C400}};
      --brand-500: {{.Style.Colors.C500}};
      --brand-600: {{.Style.Colors.C600}};
      --brand-700: {{.Style.Colors.C700}};
      --brand-800: {{.Style.Colors.C800}};
      --brand-900: {{.Style.Colors.C900}};
    }
  </style>
  <link rel="stylesheet" href="{{.TailwindAssetPath}}">
</head>
<body class="relative min-h-screen overflow-x-hidden bg-gradient-to-b from-brand-50/80 via-white to-brand-50/80 text-ink-700 antialiased">
  <div class="pointer-events-none fixed inset-0" aria-hidden="true">
    <img src="/background.png" alt="" class="absolute inset-0 h-full w-full object-cover" style="opacity: 0.68; object-position: 52% 34%;" />
    <div class="absolute inset-0 bg-gradient-to-b from-white/40 via-white/28 to-white/52"></div>
  </div>

  <main class="relative z-10 px-4 py-8 sm:py-10">
    <section class="mx-auto w-full max-w-5xl space-y-6">
      <nav aria-label="Admin navigation" class="flex flex-wrap gap-3 text-sm font-semibold">
        <a href="/admin/users" class="rounded-lg border border-brand-200 bg-white/90 px-3 py-2 text-brand-700 shadow-sm transition hover:bg-brand-50 focus:outline-none focus:ring-2 focus:ring-brand-400 focus:ring-offset-2">Users</a>
        <a href="/admin/critiques" aria-current="page" class="rounded-lg bg-brand-600 px-3 py-2 text-white shadow-sm transition hover:bg-brand-700 focus:outline-none focus:ring-2 focus:ring-brand-400 focus:ring-offset-2">Recipe Critiques</a>
      </nav>

      <div class="rounded-2xl border border-brand-100 bg-white/90 shadow-xl backdrop-blur-[2px]">
        <header class="border-b border-brand-100 p-6 sm:p-8">
          <p class="text-sm font-semibold uppercase tracking-wide text-brand-600">Admin kitchen notes</p>
          <div class="mt-2 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
            <div>
              <h1 class="font-display text-4xl font-extrabold tracking-tight text-brand-700">Recipe Critiques</h1>
              <p class="mt-2 text-ink-600">Review AI tasting notes and quality checks across generated recipes.</p>
            </div>
            <p class="rounded-full bg-brand-50 px-4 py-2 text-sm font-semibold text-brand-700">{{len .Critiques}} total</p>
          </div>
        </header>

        <div class="overflow-hidden p-4 sm:p-6">
          <table class="w-full divide-y divide-brand-100 text-left text-sm">
            <thead class="bg-brand-50 text-xs font-semibold uppercase tracking-wide text-brand-700">
              <tr>
                <th scope="col" class="px-4 py-3">Recipe</th>
                <th scope="col" class="px-4 py-3">Score</th>
                <th scope="col" class="px-4 py-3">Summary</th>
                <th scope="col" class="px-4 py-3">Details</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-brand-100">
              {{range .Critiques}}
              <tr>
                <td class="px-4 py-4 font-semibold">
                  <a href="{{.RecipeURL}}" class="text-brand-700 underline-offset-4 hover:text-brand-600 hover:underline focus:outline-none focus:ring-2 focus:ring-brand-400 focus:ring-offset-2">{{.RecipeTitle}}</a>
                </td>
                <td class="px-4 py-4">
                  <span class="inline-flex rounded-full bg-brand-50 px-3 py-1 text-sm font-semibold text-brand-700">{{.OverallScore}}/10</span>
                </td>
                <td class="max-w-md px-4 py-4 text-ink-600">{{.Summary}}</td>
                <td class="max-w-md px-4 py-4">
                  <details class="rounded-xl border border-brand-100 bg-white/90 p-3 shadow-sm">
                    <summary class="cursor-pointer font-semibold text-brand-700 focus:outline-none focus:ring-2 focus:ring-brand-400 focus:ring-offset-2">Open critique</summary>
                    <div class="mt-4 space-y-4 text-ink-700">
                      {{if .Strengths}}
                      <section>
                        <h2 class="text-xs font-semibold uppercase tracking-wide text-ink-500">Strengths</h2>
                        <ul class="mt-2 space-y-2">
                          {{range .Strengths}}
                          <li class="rounded-lg bg-brand-50 px-3 py-2">{{.}}</li>
                          {{end}}
                        </ul>
                      </section>
                      {{end}}
                      {{if .Issues}}
                      <section>
                        <h2 class="text-xs font-semibold uppercase tracking-wide text-ink-500">Issues</h2>
                        <ul class="mt-2 space-y-2">
                          {{range .Issues}}
                          <li class="rounded-lg border border-brand-100 bg-brand-50 px-3 py-2 text-ink-700"><span class="font-semibold">{{.Severity}} / {{.Category}}:</span> {{.Detail}}</li>
                          {{end}}
                        </ul>
                      </section>
                      {{end}}
                      {{if .SuggestedFixes}}
                      <section>
                        <h2 class="text-xs font-semibold uppercase tracking-wide text-ink-500">Suggested fixes</h2>
                        <ul class="mt-2 space-y-2">
                          {{range .SuggestedFixes}}
                          <li class="rounded-lg bg-brand-50 px-3 py-2">{{.}}</li>
                          {{end}}
                        </ul>
                      </section>
                      {{end}}
                    </div>
                  </details>
                </td>
              </tr>
              {{end}}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  </main>
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
			CritiquePageTheme
		}{
			Critiques:         views,
			CritiquePageTheme: currentCritiquePageTheme(),
		}); err != nil {
			slog.ErrorContext(r.Context(), "failed to render admin recipe critiques page", "error", err)
			http.Error(w, "unable to render recipe critiques", http.StatusInternalServerError)
			return
		}
	})
}

var critiquePageTmpl = template.Must(template.New("critique-page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{.RecipeTitle}} critique | Careme</title>
  <style>
    :root{
      --brand-50:  {{.Style.Colors.C50}};
      --brand-100: {{.Style.Colors.C100}};
      --brand-200: {{.Style.Colors.C200}};
      --brand-300: {{.Style.Colors.C300}};
      --brand-400: {{.Style.Colors.C400}};
      --brand-500: {{.Style.Colors.C500}};
      --brand-600: {{.Style.Colors.C600}};
      --brand-700: {{.Style.Colors.C700}};
      --brand-800: {{.Style.Colors.C800}};
      --brand-900: {{.Style.Colors.C900}};
    }
  </style>
  <link rel="stylesheet" href="{{.TailwindAssetPath}}">
</head>
<body class="relative min-h-screen overflow-x-hidden bg-gradient-to-b from-brand-50/80 via-white to-brand-50/80 text-ink-700 antialiased">
  <div class="pointer-events-none fixed inset-0" aria-hidden="true">
    <img src="/background.png" alt="" class="absolute inset-0 h-full w-full object-cover" style="opacity: 0.68; object-position: 52% 34%;" />
    <div class="absolute inset-0 bg-gradient-to-b from-white/40 via-white/28 to-white/52"></div>
  </div>

  <main class="relative z-10 px-4 py-8 sm:py-10">
    <section class="mx-auto w-full max-w-3xl space-y-6">
      <a href="{{.RecipeURL}}" class="inline-flex items-center rounded-lg border border-brand-200 bg-white/90 px-4 py-2 text-sm font-semibold text-brand-700 shadow-sm transition hover:bg-brand-50 focus:outline-none focus:ring-2 focus:ring-brand-400 focus:ring-offset-2">Back to recipe</a>

      <article class="rounded-2xl border border-brand-100 bg-white/90 shadow-xl backdrop-blur-[2px]">
        <header class="border-b border-brand-100 p-6 sm:p-8">
          <p class="text-sm font-semibold uppercase tracking-wide text-brand-600">Chef critique</p>
          <div class="mt-3 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div>
              <h1 class="font-display text-4xl font-extrabold tracking-tight text-brand-700">{{.RecipeTitle}}</h1>
              <p class="mt-3 text-lg text-ink-600">{{.Summary}}</p>
            </div>
            <p class="shrink-0 rounded-full bg-brand-50 px-4 py-2 text-sm font-semibold text-brand-700"><strong>Score:</strong> {{.OverallScore}}/10</p>
          </div>
        </header>

        <div class="space-y-6 p-6 sm:p-8">
          {{if .Strengths}}
          <section>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-ink-500">Strengths</h2>
            <ul class="mt-3 space-y-2">
              {{range .Strengths}}
              <li class="rounded-lg bg-brand-50 px-3 py-2 text-sm text-brand-800">{{.}}</li>
              {{end}}
            </ul>
          </section>
          {{end}}

          {{if .Issues}}
          <section>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-ink-500">Issues</h2>
            <ul class="mt-3 space-y-2">
              {{range .Issues}}
              <li class="rounded-lg border border-brand-100 bg-brand-50 px-3 py-2 text-sm text-ink-700"><span class="font-semibold">{{.Severity}} / {{.Category}}:</span> {{.Detail}}</li>
              {{end}}
            </ul>
          </section>
          {{end}}

          {{if .SuggestedFixes}}
          <section>
            <h2 class="text-sm font-semibold uppercase tracking-wide text-ink-500">Suggested fixes</h2>
            <ul class="mt-3 space-y-2">
              {{range .SuggestedFixes}}
              <li class="rounded-lg bg-brand-50 px-3 py-2 text-sm text-brand-800">{{.}}</li>
              {{end}}
            </ul>
          </section>
          {{end}}
        </div>
      </article>
    </section>
  </main>
</body>
</html>`))

func CritiquePage(s store, rio recipeio) http.Handler {
	if rio == nil {
		panic("store and recipeio must not be nil")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		hash := r.PathValue("hash")
		if hash == "" {
			http.Error(w, "missing recipe hash", http.StatusBadRequest)
			return
		}
		cachedCritique, err := s.Load(r.Context(), hash)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				http.Error(w, "critique not found", http.StatusNotFound)
				return
			}
			slog.ErrorContext(r.Context(), "failed to load recipe critique", "hash", hash, "error", err)
			http.Error(w, "unable to load critique", http.StatusInternalServerError)
			return
		}

		recipeTitle := "Recipe"
		if recipe, err := rio.SingleFromCache(r.Context(), hash); err == nil {
			recipeTitle = recipe.Title
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := critiquePageTmpl.Execute(w, struct {
			RecipeTitle string
			RecipeURL   string
			ai.RecipeCritique
			CritiquePageTheme
		}{
			RecipeTitle:       recipeTitle,
			RecipeURL:         "/recipe/" + hash,
			RecipeCritique:    *cachedCritique,
			CritiquePageTheme: currentCritiquePageTheme(),
		}); err != nil {
			slog.ErrorContext(r.Context(), "failed to render recipe critique page", "hash", hash, "error", err)
			http.Error(w, "unable to render critique", http.StatusInternalServerError)
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
