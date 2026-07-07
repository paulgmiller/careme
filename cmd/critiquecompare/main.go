package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/parallelism"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"

	"github.com/samber/lo"
)

const (
	defaultCompareModel = "gemini-3.5-flash"
	benchmarkPrefix     = "recipe_critique_comparisons/"
)

type recipeCritiquer interface {
	CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error)
}

type comparisonRow struct {
	Hash          string
	Title         string
	Cached        *ai.RecipeCritique
	Flash         *ai.RecipeCritique
	ScoreDelta    int
	AbsScoreDelta int
}

type candidate struct {
	hash     string
	recipe   *ai.Recipe
	critique *ai.RecipeCritique
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	var limit int
	var model string
	var refresh bool

	fs := flag.NewFlagSet("geminicritiquecompare", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.IntVar(&limit, "n", 10, "number of already-critiqued recipes to compare")
	fs.StringVar(&model, "model", defaultCompareModel, "Gemini model to use for comparison critiques")
	fs.BoolVar(&refresh, "refresh", false, "rerun comparison critiques even if benchmark results are cached")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if limit < 1 {
		return errors.New("-n must be greater than zero")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("-model is required")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	cacheStore, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}
	critiquer := ai.NewCritiquer(cfg.Gemini.APIKey, model, http.DefaultClient)

	rows, err := compareCritiques(ctx, cacheStore, critiquer, model, limit, refresh)
	if err != nil {
		return err
	}
	return printRows(out, rows)
}

func compareCritiques(
	ctx context.Context,
	cacheStore cache.ListCache,
	critiquer recipeCritiquer,
	model string,
	limit int,
	refresh bool,
) ([]comparisonRow, error) {
	critiqueStore := critique.NewStore(cacheStore)
	recipeStore := recipes.IO(cacheStore)
	benchmarkStore := newBenchmarkStore(cacheStore, model)

	candidates, err := loadCandidates(ctx, critiqueStore, recipeStore, limit)
	if err != nil {
		return nil, err
	}

	rows, err := parallelism.MapWithErrors(candidates,
		func(c candidate) (comparisonRow, error) {
			return compareCandidate(ctx, c, benchmarkStore, critiquer, model, refresh)
		})
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func compareCandidate(
	ctx context.Context,
	c candidate,
	benchmarkStore benchmarkStore,
	critiquer recipeCritiquer,
	model string,
	refresh bool,
) (comparisonRow, error) {
	flash, err := benchmarkStore.Load(ctx, c.hash)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			return comparisonRow{}, fmt.Errorf("load benchmark critique for %s: %w", c.hash, err)
		}
	}
	if refresh || flash == nil {
		flash, err = critiquer.CritiqueRecipe(ctx, *c.recipe)
		if err != nil {
			return comparisonRow{}, fmt.Errorf("run %s critique for %q (%s): %w", model, c.recipe.Title, c.hash, err)
		}
		if err := benchmarkStore.Save(ctx, c.hash, flash); err != nil {
			return comparisonRow{}, fmt.Errorf("save benchmark critique for %s: %w", c.hash, err)
		}
	}

	delta := flash.OverallScore - c.critique.OverallScore
	return comparisonRow{
		Hash:          c.hash,
		Title:         c.recipe.Title,
		Cached:        c.critique,
		Flash:         flash,
		ScoreDelta:    delta,
		AbsScoreDelta: abs(delta),
	}, nil
}

type storedCritique interface {
	Load(ctx context.Context, hash string) (*ai.RecipeCritique, error)
	ListHashes(ctx context.Context) ([]string, error)
}

type storedRecipes interface {
	SingleFromCache(ctx context.Context, hash string) (*ai.Recipe, error)
}

func loadCandidates(ctx context.Context, critiqueStore storedCritique, recipeStore storedRecipes, limit int) ([]candidate, error) {
	// this is going to pull every single one.
	hashes, err := critiqueStore.ListHashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list cached critiques: %w", err)
	}

	candidates := make([]candidate, 0, limit)
	for _, hash := range lo.Take(hashes, limit) {
		cachedCritique, err := critiqueStore.Load(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("load cached critique for %s: %w", hash, err)
		}
		recipe, err := recipeStore.SingleFromCache(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("load recipe for %s: %w", hash, err)
		}
		candidates = append(candidates, candidate{
			hash:     hash,
			recipe:   recipe,
			critique: cachedCritique,
		})
	}
	return candidates, nil
}

func printRows(out io.Writer, rows []comparisonRow) error {
	if _, err := fmt.Fprintf(out, "Compared %d recipes", len(rows)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "DELTA\tCACHED\tFLASH\tTITLE\tHASH\tCACHED_MODEL\tFLASH_MODEL\tCACHED_SUMMARY\tFLASH_SUMMARY"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(
			tw,
			"%+d\t%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.ScoreDelta,
			row.Cached.OverallScore,
			row.Flash.OverallScore,
			truncate(row.Title, 48),
			row.Hash,
			emptyDash(row.Cached.Model),
			emptyDash(row.Flash.Model),
			truncate(row.Cached.Summary, 60),
			truncate(row.Flash.Summary, 60),
		); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return printStats(out, rows)
}

type scoreStats struct {
	count          int
	cachedAverage  float64
	flashAverage   float64
	deltaAverage   float64
	cachedVariance float64
	flashVariance  float64
	deltaVariance  float64
	totalDelta     int
	totalAbsDelta  int
}

func printStats(out io.Writer, rows []comparisonRow) error {
	stats := calculateStats(rows)
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "STATS\tCACHED\tFLASH\tDELTA"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "AVERAGE\t%.2f\t%.2f\t%+.2f\n", stats.cachedAverage, stats.flashAverage, stats.deltaAverage); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "VARIANCE\t%.2f\t%.2f\t%.2f\n", stats.cachedVariance, stats.flashVariance, stats.deltaVariance); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "TOTAL_DIFFERENCE\t%d\tABS %d\n", stats.totalDelta, stats.totalAbsDelta)
	return err
}

func calculateStats(rows []comparisonRow) scoreStats {
	stats := scoreStats{count: len(rows)}
	if len(rows) == 0 {
		return stats
	}

	for _, row := range rows {
		stats.cachedAverage += float64(row.Cached.OverallScore)
		stats.flashAverage += float64(row.Flash.OverallScore)
		stats.deltaAverage += float64(row.ScoreDelta)
		stats.totalDelta += row.ScoreDelta
		stats.totalAbsDelta += row.AbsScoreDelta
	}
	count := float64(len(rows))
	stats.cachedAverage /= count
	stats.flashAverage /= count
	stats.deltaAverage /= count

	for _, row := range rows {
		stats.cachedVariance += squared(float64(row.Cached.OverallScore) - stats.cachedAverage)
		stats.flashVariance += squared(float64(row.Flash.OverallScore) - stats.flashAverage)
		stats.deltaVariance += squared(float64(row.ScoreDelta) - stats.deltaAverage)
	}
	stats.cachedVariance /= count
	stats.flashVariance /= count
	stats.deltaVariance /= count
	return stats
}

func squared(n float64) float64 {
	return n * n
}

func truncate(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	if limit <= 3 {
		return s[:limit]
	}
	return s[:limit-3] + "..."
}

func emptyDash(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return s
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

type benchmarkStore struct {
	cache cache.ListCache
	model string
}

func newBenchmarkStore(c cache.ListCache, model string) benchmarkStore {
	return benchmarkStore{
		cache: c,
		model: strings.NewReplacer("/", "_", "\\", "_").Replace(model),
	}
}

func (s benchmarkStore) key(hash string) string {
	return benchmarkPrefix + s.model + "/" + hash
}

func (s benchmarkStore) Load(ctx context.Context, hash string) (*ai.RecipeCritique, error) {
	reader, err := s.cache.Get(ctx, s.key(hash))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var critique ai.RecipeCritique
	if err := json.NewDecoder(reader).Decode(&critique); err != nil {
		return nil, err
	}
	return &critique, nil
}

func (s benchmarkStore) Save(ctx context.Context, hash string, critique *ai.RecipeCritique) error {
	if critique == nil {
		return errors.New("critique is required")
	}
	body, err := json.Marshal(critique)
	if err != nil {
		return err
	}
	return s.cache.Put(ctx, s.key(hash), string(body), cache.Unconditional())
}
