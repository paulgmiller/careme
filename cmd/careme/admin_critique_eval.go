package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
)

const (
	critiqueEvalDatasetPrefix = "recipe_critique_evals/datasets/"
	critiqueEvalResultPrefix  = "recipe_critique_evals/results/"
)

type adminCritiqueEvalDataset struct {
	ID      string                    `json:"id"`
	Name    string                    `json:"name"`
	Samples []adminCritiqueEvalSample `json:"samples"`
}

type adminCritiqueEvalSample struct {
	Hash              string             `json:"hash"`
	Title             string             `json:"title"`
	FeedbackUpdatedAt time.Time          `json:"feedback_updated_at"`
	Stars             int                `json:"stars,omitempty"`
	Historical        *ai.RecipeCritique `json:"historical_critique,omitempty"`
}

type adminCritiqueEvalResult struct {
	PromptFingerprint string             `json:"prompt_fingerprint"`
	RequestedModel    string             `json:"requested_model"`
	RecipeHash        string             `json:"recipe_hash"`
	Critique          *ai.RecipeCritique `json:"critique"`
}

type adminCritiqueEvalPageData struct {
	Datasets     []adminCritiqueEvalDataset
	Dataset      *adminCritiqueEvalDataset
	Fingerprints []string
	Fingerprint  string
	Rows         []adminCritiqueEvalRow
	Detail       *adminCritiqueEvalDetail
	Error        string
}

type adminCritiqueEvalRow struct {
	Title      string
	Hash       string
	Feedback   string
	Stars      string
	Historical string
	Scores     []adminCritiqueEvalScore
}

type adminCritiqueEvalScore struct {
	Model string
	Score string
}

type adminCritiqueEvalDetail struct {
	Title     string
	Hash      string
	Feedback  string
	Stars     string
	Critiques []adminCritiqueEvalCritique
}

type adminCritiqueEvalCritique struct {
	Heading string
	Value   *ai.RecipeCritique
}

var adminCritiqueEvalTemplate = template.Must(template.New("admin-critique-evals").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Recipe Critique Evals</title><style>
body{font-family:system-ui,sans-serif;margin:2rem;line-height:1.4}a{color:#075985}table{border-collapse:collapse;width:100%;font-size:.88rem}th,td{border:1px solid #ddd;padding:.45rem;text-align:left;vertical-align:top}th{background:#f5f5f5}.nav{display:flex;gap:1rem;flex-wrap:wrap;margin:.75rem 0}.active{font-weight:700}.muted{color:#666}.critique{border:1px solid #ddd;border-radius:.4rem;padding:1rem;margin:1rem 0}.fail{color:#b91c1c}.pass{color:#15803d}pre{white-space:pre-wrap}
</style></head><body>
<nav><a href="/admin/">Admin</a> | <a href="/admin/users">Users</a> | <a href="/admin/critique-evals">Critique evals</a></nav>
<h1>Recipe Critique Evals</h1>
{{if .Error}}<p class="fail">{{.Error}}</p>{{end}}
{{if .Datasets}}<h2>Datasets</h2><div class="nav">{{range .Datasets}}<a href="?dataset={{.Name}}" class="{{if and $.Dataset (eq $.Dataset.Name .Name)}}active{{end}}">{{.Name}} ({{len .Samples}})</a>{{end}}</div>{{end}}
{{if .Dataset}}
<p class="muted">{{.Dataset.Name}} · {{len .Dataset.Samples}} cooked recipes</p>
{{if .Fingerprints}}<div class="nav">{{range .Fingerprints}}<a href="?dataset={{$.Dataset.Name}}&prompt={{.}}" class="{{if eq $.Fingerprint .}}active{{end}}">prompt {{.}}</a>{{end}}</div>{{end}}
{{if .Detail}}
<h2>{{.Detail.Title}}</h2><p class="muted">{{.Detail.Hash}} · feedback {{.Detail.Feedback}} · stars {{.Detail.Stars}}</p>
{{range .Detail.Critiques}}<section class="critique"><h3>{{.Heading}}</h3>{{if .Value}}<p><strong>Model:</strong> {{.Value.Model}}<br><strong>Score:</strong> {{.Value.OverallScore}}/10<br><strong>Summary:</strong> {{.Value.Summary}}</p><strong>Strengths</strong><ul>{{range .Value.Strengths}}<li>{{.}}</li>{{else}}<li>None</li>{{end}}</ul><strong>Issues</strong><ul>{{range .Value.Issues}}<li>[{{.Category}}/{{.Severity}}] {{.Detail}}</li>{{else}}<li>None</li>{{end}}</ul><strong>Suggested fixes</strong><ul>{{range .Value.SuggestedFixes}}<li>{{.}}</li>{{else}}<li>None</li>{{end}}</ul>{{else}}<p class="muted">Unavailable</p>{{end}}</section>{{end}}
{{else}}
<table><thead><tr><th>Recipe</th><th>Feedback</th><th>Stars</th><th>Historical</th><th>Fresh critiques</th></tr></thead><tbody>{{range .Rows}}<tr><td><a href="?dataset={{$.Dataset.Name}}&prompt={{$.Fingerprint}}&hash={{.Hash}}">{{.Title}}</a><br><span class="muted">{{.Hash}}</span></td><td>{{.Feedback}}</td><td>{{.Stars}}</td><td>{{.Historical}}</td><td>{{range .Scores}}<div><strong>{{.Model}}</strong>: {{.Score}}</div>{{else}}<span class="muted">No result</span>{{end}}</td></tr>{{end}}</tbody></table>
{{end}}{{end}}
</body></html>`))

func adminCritiqueEvalsPage(c cache.ListCache) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, err := loadAdminCritiqueEvalPage(r.Context(), c, r.URL.Query().Get("dataset"), r.URL.Query().Get("prompt"), r.URL.Query().Get("hash"))
		if err != nil {
			data.Error = err.Error()
		}
		if err := adminCritiqueEvalTemplate.Execute(w, data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}

func loadAdminCritiqueEvalPage(ctx context.Context, c cache.ListCache, datasetName, fingerprint, hash string) (adminCritiqueEvalPageData, error) {
	datasets, err := listAdminCritiqueEvalDatasets(ctx, c)
	if err != nil {
		return adminCritiqueEvalPageData{}, err
	}
	data := adminCritiqueEvalPageData{Datasets: datasets}
	if len(datasets) == 0 {
		return data, nil
	}
	for i := range datasets {
		if datasets[i].Name == datasetName {
			data.Dataset = &datasets[i]
			break
		}
	}
	if data.Dataset == nil {
		data.Dataset = &datasets[0]
	}
	results, err := listAdminCritiqueEvalResults(ctx, c, data.Dataset.ID)
	if err != nil {
		return data, err
	}
	fingerprintSet := make(map[string]struct{})
	counts := make(map[string]int)
	for _, result := range results {
		fingerprintSet[result.PromptFingerprint] = struct{}{}
		counts[result.PromptFingerprint]++
	}
	for value := range fingerprintSet {
		data.Fingerprints = append(data.Fingerprints, value)
	}
	sort.Slice(data.Fingerprints, func(i, j int) bool {
		return counts[data.Fingerprints[i]] > counts[data.Fingerprints[j]]
	})
	if fingerprint == "" && len(data.Fingerprints) > 0 {
		fingerprint = data.Fingerprints[0]
	}
	data.Fingerprint = fingerprint
	byHash := make(map[string][]adminCritiqueEvalResult)
	for _, result := range results {
		if result.PromptFingerprint == fingerprint {
			byHash[result.RecipeHash] = append(byHash[result.RecipeHash], result)
		}
	}
	for _, values := range byHash {
		sort.Slice(values, func(i, j int) bool { return values[i].RequestedModel < values[j].RequestedModel })
	}
	if hash != "" {
		for _, sample := range data.Dataset.Samples {
			if sample.Hash != hash {
				continue
			}
			detail := &adminCritiqueEvalDetail{Title: sample.Title, Hash: sample.Hash, Feedback: sample.FeedbackUpdatedAt.Format(time.RFC3339), Stars: adminStars(sample.Stars)}
			detail.Critiques = append(detail.Critiques, adminCritiqueEvalCritique{Heading: "Historical critique", Value: sample.Historical})
			for _, result := range byHash[hash] {
				detail.Critiques = append(detail.Critiques, adminCritiqueEvalCritique{Heading: "Fresh critique: " + result.RequestedModel, Value: result.Critique})
			}
			data.Detail = detail
			return data, nil
		}
		return data, fmt.Errorf("recipe %q is not in dataset %q", hash, data.Dataset.Name)
	}
	for _, sample := range data.Dataset.Samples {
		row := adminCritiqueEvalRow{Title: sample.Title, Hash: sample.Hash, Feedback: sample.FeedbackUpdatedAt.Format(time.RFC3339), Stars: adminStars(sample.Stars), Historical: adminCritiqueCell(sample.Historical)}
		for _, result := range byHash[sample.Hash] {
			row.Scores = append(row.Scores, adminCritiqueEvalScore{Model: result.RequestedModel, Score: adminCritiqueCell(result.Critique)})
		}
		data.Rows = append(data.Rows, row)
	}
	return data, nil
}

func listAdminCritiqueEvalDatasets(ctx context.Context, c cache.ListCache) ([]adminCritiqueEvalDataset, error) {
	keys, err := c.List(ctx, critiqueEvalDatasetPrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list critique eval datasets: %w", err)
	}
	datasets := make([]adminCritiqueEvalDataset, 0, len(keys))
	for _, key := range keys {
		reader, err := c.Get(ctx, critiqueEvalDatasetPrefix+key)
		if err != nil {
			return nil, err
		}
		var dataset adminCritiqueEvalDataset
		decodeErr := json.NewDecoder(reader).Decode(&dataset)
		_ = reader.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode critique eval dataset %s: %w", key, decodeErr)
		}
		datasets = append(datasets, dataset)
	}
	sort.Slice(datasets, func(i, j int) bool { return datasets[i].Name > datasets[j].Name })
	return datasets, nil
}

func listAdminCritiqueEvalResults(ctx context.Context, c cache.ListCache, datasetID string) ([]adminCritiqueEvalResult, error) {
	keys, err := c.List(ctx, critiqueEvalResultPrefix+datasetID+"/", "")
	if err != nil {
		return nil, fmt.Errorf("list critique eval results: %w", err)
	}
	results := make([]adminCritiqueEvalResult, 0, len(keys))
	for _, key := range keys {
		reader, err := c.Get(ctx, critiqueEvalResultPrefix+datasetID+"/"+key)
		if err != nil {
			return nil, err
		}
		var result adminCritiqueEvalResult
		decodeErr := json.NewDecoder(reader).Decode(&result)
		_ = reader.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode critique eval result %s: %w", key, decodeErr)
		}
		results = append(results, result)
	}
	return results, nil
}

func adminStars(stars int) string {
	if stars == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", stars)
}

func adminCritiqueCell(value *ai.RecipeCritique) string {
	if value == nil {
		return "-"
	}
	status := "fail"
	if value.OverallScore >= 8 {
		status = "pass"
	}
	return fmt.Sprintf("%d/%s/%s", value.OverallScore, status, strings.TrimSpace(value.Model))
}
