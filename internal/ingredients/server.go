package ingredients

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"careme/internal/cache"
	"careme/internal/ingredientcoverage"
	"careme/internal/kroger"
	"careme/internal/recipes"
	"careme/internal/routing"
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
	DatasetLinks    []inspectorLink
	MatcherLinks    []inspectorLink
	JSONHref        string
	TSVHref         string
	Sections        []analysisSectionView
}

type inspectorLink struct {
	Label  string
	Href   string
	Active bool
}

type analysisSectionView struct {
	DatasetName        string
	DatasetLabel       string
	ComparisonMode     bool
	Baseline           reportView
	Stemmed            *reportView
	AddedTerms         []string
	RemovedTerms       []string
	AddedIngredients   []string
	RemovedIngredients []string
}

type reportView struct {
	MatcherLabel           string
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
        {{range .DatasetLinks}}
        <a class="pill{{if .Active}} active{{end}}" href="{{.Href}}">{{.Label}}</a>
        {{end}}
      </div>
      <div class="pillbar">
        {{range .MatcherLinks}}
        <a class="pill{{if .Active}} active{{end}}" href="{{.Href}}">{{.Label}}</a>
        {{end}}
      </div>
      <p><a href="{{.JSONHref}}">Raw JSON</a> · <a href="{{.TSVHref}}">TSV export</a></p>
    </div>

    {{range .Sections}}
    <section class="panel">
      <h2>{{.DatasetLabel}}</h2>
      <div class="grid{{if .ComparisonMode}} two{{end}}">
        <div>
          <h3>{{.Baseline.MatcherLabel}}</h3>
          <div class="stats">
            <div class="stat"><span class="muted">Score</span><strong>{{.Baseline.Score}}</strong><span>{{.Baseline.MatchedTerms}} / {{.Baseline.TotalTerms}} terms</span></div>
            <div class="stat"><span class="muted">Matched ingredients</span><strong>{{.Baseline.MatchedIngredientCount}}</strong><span>{{.Baseline.TotalIngredients}} total</span></div>
            <div class="stat"><span class="muted">Term hits</span><strong>{{.Baseline.TotalMatches}}</strong><span>term to ingredient matches</span></div>
            <div class="stat"><span class="muted">Unmatched ingredients</span><strong>{{len .Baseline.UnmatchedIngredients}}</strong><span>no test terms hit</span></div>
          </div>
          <div class="grid two">
            <div class="listbox">
              <h3>Matched terms</h3>
              {{if .Baseline.TermMatches}}
              <ul class="tight">
                {{range .Baseline.TermMatches}}
                <li><strong>{{.Term}}</strong>: {{range $index, $match := .Matches}}{{if $index}}, {{end}}{{$match}}{{end}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">No matched terms.</p>
              {{end}}
            </div>
            <div class="listbox">
              <h3>Missing terms</h3>
              {{if .Baseline.MissingTerms}}
              <ul class="tight">
                {{range .Baseline.MissingTerms}}
                <li>{{.}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">All terms matched.</p>
              {{end}}
            </div>
            <div class="listbox">
              <h3>Ingredients with term hits</h3>
              {{if .Baseline.MatchedIngredients}}
              <ul class="tight">
                {{range .Baseline.MatchedIngredients}}
                <li><strong>{{.Description}}</strong>: {{range $index, $term := .Terms}}{{if $index}}, {{end}}{{$term}}{{end}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">No ingredients matched a term.</p>
              {{end}}
            </div>
            <div class="listbox">
              <h3>Ingredients with no term hits</h3>
              {{if .Baseline.UnmatchedIngredients}}
              <ul class="tight">
                {{range .Baseline.UnmatchedIngredients}}
                <li>{{.}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">Every ingredient matched at least one term.</p>
              {{end}}
            </div>
          </div>
        </div>

        {{if .ComparisonMode}}
        <div>
          <h3>{{.Stemmed.MatcherLabel}}</h3>
          <div class="stats">
            <div class="stat"><span class="muted">Score</span><strong>{{.Stemmed.Score}}</strong><span>{{.Stemmed.MatchedTerms}} / {{.Stemmed.TotalTerms}} terms</span></div>
            <div class="stat"><span class="muted">Matched ingredients</span><strong>{{.Stemmed.MatchedIngredientCount}}</strong><span>{{.Stemmed.TotalIngredients}} total</span></div>
            <div class="stat"><span class="muted">Term hits</span><strong>{{.Stemmed.TotalMatches}}</strong><span>term to ingredient matches</span></div>
            <div class="stat"><span class="muted">Unmatched ingredients</span><strong>{{len .Stemmed.UnmatchedIngredients}}</strong><span>no test terms hit</span></div>
          </div>
          <div class="grid two">
            <div class="listbox">
              <h3>Stemmer gained</h3>
              {{if or .AddedTerms .AddedIngredients}}
              <p><strong>Terms</strong></p>
              {{if .AddedTerms}}
              <ul class="tight">
                {{range .AddedTerms}}
                <li>{{.}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">No additional terms.</p>
              {{end}}
              <p><strong>Ingredients</strong></p>
              {{if .AddedIngredients}}
              <ul class="tight">
                {{range .AddedIngredients}}
                <li>{{.}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">No additional ingredients.</p>
              {{end}}
              {{else}}
              <p class="muted">No additional matches.</p>
              {{end}}
            </div>
            <div class="listbox">
              <h3>Stemmer lost</h3>
              {{if or .RemovedTerms .RemovedIngredients}}
              <p><strong>Terms</strong></p>
              {{if .RemovedTerms}}
              <ul class="tight">
                {{range .RemovedTerms}}
                <li>{{.}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">No lost terms.</p>
              {{end}}
              <p><strong>Ingredients</strong></p>
              {{if .RemovedIngredients}}
              <ul class="tight">
                {{range .RemovedIngredients}}
                <li>{{.}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">No lost ingredients.</p>
              {{end}}
              {{else}}
              <p class="muted">No regressions.</p>
              {{end}}
            </div>
            <div class="listbox">
              <h3>Stemmed matched terms</h3>
              {{if .Stemmed.TermMatches}}
              <ul class="tight">
                {{range .Stemmed.TermMatches}}
                <li><strong>{{.Term}}</strong>: {{range $index, $match := .Matches}}{{if $index}}, {{end}}{{$match}}{{end}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">No matched terms.</p>
              {{end}}
            </div>
            <div class="listbox">
              <h3>Stemmed unmatched ingredients</h3>
              {{if .Stemmed.UnmatchedIngredients}}
              <ul class="tight">
                {{range .Stemmed.UnmatchedIngredients}}
                <li>{{.}}</li>
                {{end}}
              </ul>
              {{else}}
              <p class="muted">Every ingredient matched at least one term.</p>
              {{end}}
            </div>
          </div>
        </div>
        {{end}}
      </div>
    </section>
    {{end}}
  </main>
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
	comparisonMode, err := parseComparisonMode(r.URL.Query().Get("matcher"))
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
		DatasetLinks:    buildDatasetLinks(r.URL, selectedDatasetName(r.URL.Query().Get("dataset"))),
		MatcherLinks:    buildMatcherLinks(r.URL, comparisonMode),
		JSONHref:        withQuery(r.URL, map[string]string{"format": "json"}),
		TSVHref:         withQuery(r.URL, map[string]string{"format": "tsv"}),
		Sections:        buildSections(selectedDatasets, ingredients, comparisonMode),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := inspectorTemplate.Execute(w, pageData); err != nil {
		slog.ErrorContext(ctx, "failed to render ingredients inspector", "hash", hash, "error", err)
		http.Error(w, "failed to render ingredient inspector", http.StatusInternalServerError)
	}
}

func parseComparisonMode(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "baseline":
		return false, nil
	case "compare":
		return true, nil
	default:
		return false, fmt.Errorf("unsupported matcher %q", value)
	}
}

func selectedDatasetName(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ingredientcoverage.DefaultDatasetName()
	}
	return normalized
}

func buildDatasetLinks(current *url.URL, active string) []inspectorLink {
	links := []inspectorLink{
		{Label: "Produce", Href: withQuery(current, map[string]string{"dataset": "produce", "format": ""}), Active: active == "produce"},
		{Label: "Meat", Href: withQuery(current, map[string]string{"dataset": "meat", "format": ""}), Active: active == "meat"},
		{Label: "Seafood", Href: withQuery(current, map[string]string{"dataset": "seafood", "format": ""}), Active: active == "seafood"},
		{Label: "All", Href: withQuery(current, map[string]string{"dataset": "all", "format": ""}), Active: active == "all"},
	}
	return links
}

func buildMatcherLinks(current *url.URL, comparisonMode bool) []inspectorLink {
	return []inspectorLink{
		{Label: "Baseline", Href: withQuery(current, map[string]string{"matcher": "baseline", "format": ""}), Active: !comparisonMode},
		{Label: "Compare stemmer", Href: withQuery(current, map[string]string{"matcher": "compare", "format": ""}), Active: comparisonMode},
	}
}

func withQuery(current *url.URL, updates map[string]string) string {
	copyURL := *current
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

func buildSections(datasets []ingredientcoverage.Dataset, ingredients []kroger.Ingredient, comparisonMode bool) []analysisSectionView {
	sections := make([]analysisSectionView, 0, len(datasets))
	for _, dataset := range datasets {
		baseline := ingredientcoverage.Analyze(dataset, ingredients, ingredientcoverage.BaselineMatcher())
		section := analysisSectionView{
			DatasetName:    dataset.Name,
			DatasetLabel:   dataset.Label,
			ComparisonMode: comparisonMode,
			Baseline:       reportFromCoverage(baseline),
		}
		if comparisonMode {
			comparison := ingredientcoverage.Compare(dataset, ingredients)
			stemmed := reportFromCoverage(comparison.Stemmed)
			section.Stemmed = &stemmed
			section.AddedTerms = comparison.AddedTerms
			section.RemovedTerms = comparison.RemovedTerms
			section.AddedIngredients = comparison.AddedIngredients
			section.RemovedIngredients = comparison.RemovedIngredients
		}
		sections = append(sections, section)
	}
	return sections
}

func reportFromCoverage(report ingredientcoverage.Report) reportView {
	return reportView{
		MatcherLabel:           report.MatcherLabel,
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
