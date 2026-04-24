package ingredients

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"careme/internal/cache"
	"careme/internal/ingredientcoverage"
	"careme/internal/kroger"
	"careme/internal/recipes"
	"careme/internal/routing"
	"careme/internal/templates"
)

type server struct {
	cache cache.Cache
}

type inspectorPageData struct {
	Hash            string
	LocationHash    string
	LocationID      string
	LocationName    string
	Date            string
	IngredientCount int
	ViewLinks       []inspectorLink
	DatasetLinks    []inspectorLink
	JSONHref        string
	TSVHref         string
	ClerkRefresh    template.HTML
	Sections        []analysisSectionView
	Unmatched       *globalUnmatchedView
}

type inspectorLink struct {
	Label  string
	Href   string
	Active bool
}

type analysisSectionView struct {
	DatasetName  string
	DatasetLabel string
	Report       reportView
}

type globalUnmatchedView struct {
	MatchedIngredientCount int
	UnmatchedCount         int
	Ingredients            []string
}

type reportView struct {
	MatchedTerms           int
	TotalTerms             int
	Score                  string
	TotalIngredients       int
	MatchedIngredientCount int
	TotalMatches           int
	MissingTerms           []string
	TermMatches            []ingredientcoverage.TermMatch
	MatchedIngredients     []ingredientcoverage.IngredientMatch
	UnmatchedIngredients   []string
}

var inspectorTemplate = template.Must(template.New("ingredient-inspector").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Ingredient Coverage Inspector</title>
  <style>
    body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 0; background: #f6f4ee; color: #1f2937; }
    main { max-width: 1200px; margin: 0 auto; padding: 24px; }
    h1, h2, h3 { margin: 0 0 12px; }
    p, ul { margin-top: 0; }
    a { color: #0f766e; }
    .panel { background: #fffdfa; border: 1px solid #d6d3d1; border-radius: 12px; padding: 16px; margin-bottom: 16px; }
    .pillbar { display: flex; flex-wrap: wrap; gap: 8px; margin: 12px 0; }
    .pill { display: inline-block; padding: 6px 10px; border: 1px solid #cbd5e1; border-radius: 999px; text-decoration: none; color: #334155; background: #ffffff; }
    .pill.active { background: #0f766e; border-color: #0f766e; color: #ffffff; }
    .grid { display: grid; gap: 16px; }
    .grid.two { grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); }
    .stats { display: grid; gap: 12px; grid-template-columns: repeat(auto-fit, minmax(170px, 1fr)); margin-bottom: 16px; }
    .stat { background: #fafaf9; border: 1px solid #e7e5e4; border-radius: 10px; padding: 12px; }
    .stat strong { display: block; font-size: 1.2rem; }
    .listbox { background: #fafaf9; border: 1px solid #e7e5e4; border-radius: 10px; padding: 12px; }
    .muted { color: #6b7280; }
    .tight li { margin-bottom: 6px; }
    code { background: #f1f5f9; padding: 1px 4px; border-radius: 4px; }
  </style>
</head>
<body>
  <main>
    <div class="panel">
      <h1>Ingredient Coverage Inspector</h1>
      <p class="muted">Recipe hash <code>{{.Hash}}</code></p>
      <p>Store: <strong>{{if .LocationName}}{{.LocationName}}{{else}}Unknown store{{end}}</strong>{{if .LocationID}} (<code>{{.LocationID}}</code>){{end}} {{if .Date}}for {{.Date}}{{end}}</p>
      <p class="muted">Location ingredient cache <code>{{.LocationHash}}</code> with {{.IngredientCount}} ingredients.</p>
      <div class="pillbar">
        {{range .ViewLinks}}
        <a class="pill{{if .Active}} active{{end}}" href="{{.Href}}">{{.Label}}</a>
        {{end}}
      </div>
      {{if .Sections}}
      <div class="pillbar">
        {{range .DatasetLinks}}
        <a class="pill{{if .Active}} active{{end}}" href="{{.Href}}">{{.Label}}</a>
        {{end}}
      </div>
      {{end}}
      <p><a href="{{.JSONHref}}">Raw JSON</a> · <a href="{{.TSVHref}}">TSV export</a></p>
    </div>

    {{if .Unmatched}}
    <section class="panel">
      <h2>Unmatched Across All Categories</h2>
      <div class="stats">
        <div class="stat"><span class="muted">Total ingredients</span><strong>{{.IngredientCount}}</strong><span>cached for this location</span></div>
        <div class="stat"><span class="muted">Matched anywhere</span><strong>{{.Unmatched.MatchedIngredientCount}}</strong><span>produce, meat, or seafood</span></div>
        <div class="stat"><span class="muted">No term hits</span><strong>{{.Unmatched.UnmatchedCount}}</strong><span>across all categories</span></div>
      </div>
      <div class="listbox">
        <h3>Ingredients with no term hits anywhere</h3>
        {{if .Unmatched.Ingredients}}
        <ul class="tight">
          {{range .Unmatched.Ingredients}}
          <li>{{.}}</li>
          {{end}}
        </ul>
        {{else}}
        <p class="muted">Every ingredient matched at least one produce, meat, or seafood term.</p>
        {{end}}
      </div>
    </section>
    {{end}}

    {{range .Sections}}
    <section class="panel">
      <h2>{{.DatasetLabel}}</h2>
      <div class="grid two">
        <div>
          <h3>Matched</h3>
          <div class="stats">
            <div class="stat"><span class="muted">Score</span><strong>{{.Report.Score}}</strong><span>{{.Report.MatchedTerms}} / {{.Report.TotalTerms}} terms</span></div>
            <div class="stat"><span class="muted">Matched ingredients</span><strong>{{.Report.MatchedIngredientCount}}</strong><span>{{.Report.TotalIngredients}} total</span></div>
            <div class="stat"><span class="muted">Term hits</span><strong>{{.Report.TotalMatches}}</strong><span>term to ingredient matches</span></div>
          </div>
          <div class="listbox">
            <h3>Matched terms</h3>
            {{if .Report.TermMatches}}
            <ul class="tight">
              {{range .Report.TermMatches}}
              <li><strong>{{.Term}}</strong>: {{range $index, $match := .Matches}}{{if $index}}, {{end}}{{$match}}{{end}}</li>
              {{end}}
            </ul>
            {{else}}
            <p class="muted">No matched terms.</p>
            {{end}}
          </div>
          <div class="listbox">
            <h3>Ingredients with term hits</h3>
            {{if .Report.MatchedIngredients}}
            <ul class="tight">
              {{range .Report.MatchedIngredients}}
              <li><strong>{{.Description}}</strong>: {{range $index, $term := .Terms}}{{if $index}}, {{end}}{{$term}}{{end}}</li>
              {{end}}
            </ul>
            {{else}}
            <p class="muted">No ingredients matched a term.</p>
            {{end}}
          </div>
        </div>
        <div>
          <h3>Coverage Gaps</h3>
          <div class="stats">
            <div class="stat"><span class="muted">Missing terms</span><strong>{{len .Report.MissingTerms}}</strong><span>no ingredient hits</span></div>
            <div class="stat"><span class="muted">Matched ingredients</span><strong>{{.Report.MatchedIngredientCount}}</strong><span>items hit by this dataset</span></div>
          </div>
          <div class="listbox">
            <h3>Missing terms</h3>
            {{if .Report.MissingTerms}}
            <ul class="tight">
              {{range .Report.MissingTerms}}
              <li>{{.}}</li>
              {{end}}
            </ul>
            {{else}}
            <p class="muted">All terms matched.</p>
            {{end}}
          </div>
        </div>
      </div>
    </section>
    {{end}}
  </main>
  {{.ClerkRefresh}}
</body>
</html>`))

func NewHandler(c cache.Cache) *server {
	return &server{cache: c}
}

func (s *server) Register(mux routing.Registrar) {
	mux.HandleFunc("GET /ingredients/{hash}", s.handleIngredients)
}

func (s *server) handleIngredients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := r.PathValue("hash")
	rio := recipes.IO(s.cache)

	params, err := rio.ParamsFromCache(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "parameters not found in cache", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load params for hash", "hash", hash, "error", err)
		http.Error(w, "failed to fetch params", http.StatusInternalServerError)
		return
	}

	locationHash := params.LocationHash()
	ingredients, err := rio.IngredientsFromCache(ctx, locationHash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "ingredients not found in cache", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load ingredients for hash", "hash", locationHash, "error", err)
		http.Error(w, "failed to fetch ingredients", http.StatusInternalServerError)
		return
	}

	slog.Info("serving cached ingredients", "location", params.String(), "hash", locationHash)
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	switch format {
	case "tsv":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := kroger.ToTSV(ingredients, w); err != nil {
			http.Error(w, "failed to encode ingredients", http.StatusInternalServerError)
		}
		return
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(ingredients); err != nil {
			http.Error(w, "failed to encode ingredients", http.StatusInternalServerError)
			return
		}
		return
	case "", "html":
	default:
		http.Error(w, fmt.Sprintf("unsupported format %q", format), http.StatusBadRequest)
		return
	}

	selectedDatasets, err := ingredientcoverage.DatasetsForSelection(r.URL.Query().Get("dataset"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	view, err := selectedView(r.URL.Query().Get("view"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pageData := inspectorPageData{
		Hash:            hash,
		LocationHash:    locationHash,
		LocationID:      locationID(params),
		LocationName:    locationName(params),
		Date:            params.Date.Format("2006-01-02"),
		IngredientCount: len(ingredients),
		ViewLinks:       buildViewLinks(r.URL, view),
		JSONHref:        withQuery(r.URL, map[string]string{"format": "json"}),
		TSVHref:         withQuery(r.URL, map[string]string{"format": "tsv"}),
		ClerkRefresh:    templates.ClerkRefreshHTML(true),
	}
	if view == "coverage" {
		pageData.DatasetLinks = buildDatasetLinks(r.URL, selectedDatasetName(r.URL.Query().Get("dataset")))
		pageData.Sections = buildSections(selectedDatasets, ingredients)
	} else {
		pageData.Unmatched = buildGlobalUnmatched(ingredients)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := inspectorTemplate.Execute(w, pageData); err != nil {
		slog.ErrorContext(ctx, "failed to render ingredients inspector", "hash", hash, "error", err)
		http.Error(w, "failed to render ingredient inspector", http.StatusInternalServerError)
	}
}

func selectedDatasetName(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ingredientcoverage.DefaultDatasetName()
	}
	return normalized
}

func selectedView(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "coverage":
		return "coverage", nil
	case "unmatched":
		return "unmatched", nil
	default:
		return "", fmt.Errorf("unsupported view %q", value)
	}
}

func buildViewLinks(current *url.URL, active string) []inspectorLink {
	return []inspectorLink{
		{Label: "Coverage", Href: withQuery(current, map[string]string{"view": "coverage", "format": ""}), Active: active == "coverage"},
		{Label: "Unmatched", Href: withQuery(current, map[string]string{"view": "unmatched", "format": ""}), Active: active == "unmatched"},
	}
}

func buildDatasetLinks(current *url.URL, active string) []inspectorLink {
	links := []inspectorLink{
		{Label: "Produce", Href: withQuery(current, map[string]string{"dataset": "produce", "view": "coverage", "format": ""}), Active: active == "produce"},
		{Label: "Meat", Href: withQuery(current, map[string]string{"dataset": "meat", "view": "coverage", "format": ""}), Active: active == "meat"},
		{Label: "Seafood", Href: withQuery(current, map[string]string{"dataset": "seafood", "view": "coverage", "format": ""}), Active: active == "seafood"},
	}
	return links
}

func withQuery(current *url.URL, updates map[string]string) string {
	copyURL := *current
	copyURL.Path = adminPath(copyURL.Path)
	query := copyURL.Query()
	for key, value := range updates {
		if value == "" {
			query.Del(key)
			continue
		}
		query.Set(key, value)
	}
	copyURL.RawQuery = query.Encode()
	return copyURL.String()
}

func adminPath(path string) string {
	if strings.HasPrefix(path, "/admin/") || path == "/admin" {
		return path
	}
	if strings.HasPrefix(path, "/") {
		return "/admin" + path
	}
	return "/admin/" + path
}

func buildSections(datasets []ingredientcoverage.Dataset, ingredients []kroger.Ingredient) []analysisSectionView {
	sections := make([]analysisSectionView, 0, len(datasets))
	for _, dataset := range datasets {
		report := ingredientcoverage.Analyze(dataset, ingredients, ingredientcoverage.BaselineMatcher())
		sections = append(sections, analysisSectionView{
			DatasetName:  dataset.Name,
			DatasetLabel: dataset.Label,
			Report:       reportFromCoverage(report),
		})
	}
	return sections
}

func buildGlobalUnmatched(ingredients []kroger.Ingredient) *globalUnmatchedView {
	allDatasets := ingredientcoverage.Datasets()
	matched := make(map[string]struct{})
	for _, dataset := range allDatasets {
		report := ingredientcoverage.Analyze(dataset, ingredients, ingredientcoverage.BaselineMatcher())
		for _, ingredient := range report.MatchedIngredients {
			matched[ingredient.Description] = struct{}{}
		}
	}

	allDescriptions := make(map[string]struct{})
	unmatched := make([]string, 0)
	for _, ingredient := range ingredients {
		if ingredient.Description == nil {
			continue
		}
		description := strings.TrimSpace(*ingredient.Description)
		if description == "" {
			continue
		}
		if _, ok := allDescriptions[description]; ok {
			continue
		}
		allDescriptions[description] = struct{}{}
		if _, ok := matched[description]; ok {
			continue
		}
		unmatched = append(unmatched, description)
	}
	slices.Sort(unmatched)
	return &globalUnmatchedView{
		MatchedIngredientCount: len(allDescriptions) - len(unmatched),
		UnmatchedCount:         len(unmatched),
		Ingredients:            unmatched,
	}
}

func reportFromCoverage(report ingredientcoverage.Report) reportView {
	return reportView{
		MatchedTerms:           report.MatchedTerms,
		TotalTerms:             report.TotalTerms,
		Score:                  fmt.Sprintf("%.3f", report.Score),
		TotalIngredients:       report.TotalIngredients,
		MatchedIngredientCount: report.MatchedIngredientCount,
		TotalMatches:           report.TotalMatches,
		MissingTerms:           report.MissingTerms,
		TermMatches:            report.TermMatches,
		MatchedIngredients:     report.MatchedIngredients,
		UnmatchedIngredients:   report.UnmatchedIngredients,
	}
}

func locationName(params *recipes.GeneratorParams) string {
	if params == nil || params.Location == nil {
		return ""
	}
	return params.Location.Name
}

func locationID(params *recipes.GeneratorParams) string {
	if params == nil || params.Location == nil {
		return ""
	}
	return params.Location.ID
}
