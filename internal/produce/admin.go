package produce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"careme/internal/cache"
	"careme/internal/recipes"
)

const (
	defaultAdminDays = 7
	minAdminDays     = 1
	maxAdminDays     = 90
	paramsPrefix     = "params/"
)

type LocationScore struct {
	LocationID         string        `json:"location_id"`
	LocationName       string        `json:"location_name,omitempty"`
	Chain              string        `json:"chain,omitempty"`
	ZipCode            string        `json:"zip_code,omitempty"`
	Date               string        `json:"date"`
	ProduceScore       Score         `json:"produce_score"`
	TopMissingFamilies []string      `json:"top_missing_families"`
	TopMatchedFamilies []FamilyScore `json:"top_matched_families"`
}

func AdminScoresPage(c cache.ListCache) http.Handler {
	if c == nil {
		panic("cache must not be nil")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		days, err := parseDays(r.URL.Query().Get("days"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		scores, err := LoadScores(r.Context(), c, days, time.Now())
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to load produce scores", "days", days, "error", err)
			http.Error(w, "unable to load produce scores", http.StatusInternalServerError)
			return
		}

		if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "json") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			if err := enc.Encode(struct {
				Days   int             `json:"days"`
				Scores []LocationScore `json:"scores"`
			}{Days: days, Scores: scores}); err != nil {
				slog.ErrorContext(r.Context(), "failed to encode produce scores", "error", err)
			}
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := adminScoresPageTmpl.Execute(w, struct {
			Days   int
			Scores []LocationScore
		}{Days: days, Scores: scores}); err != nil {
			slog.ErrorContext(r.Context(), "failed to render produce scores page", "error", err)
			http.Error(w, "unable to render produce scores", http.StatusInternalServerError)
			return
		}
	})
}

func LoadScores(ctx context.Context, c cache.ListCache, days int, now time.Time) ([]LocationScore, error) {
	if days < minAdminDays || days > maxAdminDays {
		return nil, fmt.Errorf("days must be between %d and %d", minAdminDays, maxAdminDays)
	}

	hashes, err := c.List(ctx, paramsPrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list cached params: %w", err)
	}

	rio := recipes.IO(c)
	cutoff := dateOnly(now).AddDate(0, 0, -days)
	byLocation := make(map[string]LocationScore)
	for _, hash := range hashes {
		params, err := rio.ParamsFromCache(ctx, hash)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("load params %q: %w", hash, err)
		}
		if params.Location == nil || strings.TrimSpace(params.Location.ID) == "" {
			continue
		}
		if dateOnly(params.Date).Before(cutoff) {
			continue
		}

		locationHash, ok := safeLocationHash(params)
		if !ok {
			slog.InfoContext(ctx, "skipping produce score for unsupported cached location", "location_id", params.Location.ID)
			continue
		}

		ingredients, err := rio.IngredientsFromCache(ctx, locationHash)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("load ingredients for %q: %w", locationHash, err)
		}

		current, ok := byLocation[params.Location.ID]
		if ok && !dateOnly(params.Date).After(parseScoreDate(current.Date)) {
			continue
		}

		score := ScoreIngredients(ingredients)
		byLocation[params.Location.ID] = LocationScore{
			LocationID:         params.Location.ID,
			LocationName:       params.Location.Name,
			Chain:              params.Location.Chain,
			ZipCode:            params.Location.ZipCode,
			Date:               params.Date.Format("2006-01-02"),
			ProduceScore:       score,
			TopMissingFamilies: firstStrings(score.MissingFamilies, 10),
			TopMatchedFamilies: score.TopMatchedFamilies,
		}
	}

	scores := make([]LocationScore, 0, len(byLocation))
	for _, score := range byLocation {
		scores = append(scores, score)
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].ProduceScore.Score == scores[j].ProduceScore.Score {
			return scores[i].LocationID < scores[j].LocationID
		}
		return scores[i].ProduceScore.Score > scores[j].ProduceScore.Score
	})
	return scores, nil
}

func safeLocationHash(params *recipes.GeneratorParams) (hash string, ok bool) {
	defer func() {
		if recover() != nil {
			hash = ""
			ok = false
		}
	}()
	return params.LocationHash(), true
}

func parseDays(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultAdminDays, nil
	}
	days, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("days must be a number")
	}
	if days < minAdminDays || days > maxAdminDays {
		return 0, fmt.Errorf("days must be between %d and %d", minAdminDays, maxAdminDays)
	}
	return days, nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func parseScoreDate(raw string) time.Time {
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func firstStrings(values []string, count int) []string {
	if len(values) <= count {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:count]...)
}

var adminScoresPageTmpl = template.Must(template.New("admin-produce-scores").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Admin Produce Scores</title>
</head>
<body>
  <nav>
    <a href="/admin/users">Users</a> |
    <a href="/admin/critiques">Recipe Critiques</a> |
    <a href="/admin/produce-scores">Produce Scores</a>
  </nav>
  <h1>Produce Scores</h1>
  <p>Locations with cached ingredients in the last {{.Days}} days: {{len .Scores}}</p>
  <table border="1" cellpadding="6" cellspacing="0">
    <thead>
      <tr>
        <th>Location</th>
        <th>Date</th>
        <th>Score</th>
        <th>Families</th>
        <th>Ingredients</th>
        <th>Avg grade</th>
        <th>Missing</th>
        <th>Top matches</th>
      </tr>
    </thead>
    <tbody>
      {{range .Scores}}
      <tr>
        <td>
          <strong>{{.LocationName}}</strong><br>
          {{.LocationID}}{{if .Chain}} / {{.Chain}}{{end}}{{if .ZipCode}} / {{.ZipCode}}{{end}}
        </td>
        <td>{{.Date}}</td>
        <td>{{printf "%.1f" .ProduceScore.Score}}</td>
        <td>{{.ProduceScore.MatchedFamilies}} / {{.ProduceScore.FamilyCount}}</td>
        <td>{{.ProduceScore.IngredientCount}} total; {{.ProduceScore.GradedCount}} graded; {{.ProduceScore.UngradedCount}} ungraded</td>
        <td>{{printf "%.1f" .ProduceScore.MatchedGradeAvg}}</td>
        <td>
          {{if .TopMissingFamilies}}
          <ul>
            {{range .TopMissingFamilies}}<li>{{.}}</li>{{end}}
          </ul>
          {{else}}
          none
          {{end}}
        </td>
        <td>
          {{if .TopMatchedFamilies}}
          <ol>
            {{range .TopMatchedFamilies}}
            <li>{{.Family}}: {{.BestDescription}} ({{.BestGrade}}/10)</li>
            {{end}}
          </ol>
          {{else}}
          none
          {{end}}
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>`))
