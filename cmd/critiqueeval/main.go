package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/feedback"
	"careme/internal/users"

	"github.com/paulgmiller/kage/pkg/kage"
)

const (
	evalPrefix       = "recipe_critique_evals/"
	datasetPrefix    = evalPrefix + "datasets/"
	resultPrefix     = evalPrefix + "results/"
	defaultLimit     = 20
	defaultWorkers   = 3
	minimumPassScore = 8
)

type recipeCritiquer interface {
	CritiqueRecipe(context.Context, ai.Recipe) (*ai.RecipeCritique, error)
}

type evalSample struct {
	Hash               string             `json:"hash"`
	Title              string             `json:"title"`
	Recipe             ai.Recipe          `json:"recipe"`
	FeedbackUpdatedAt  time.Time          `json:"feedback_updated_at"`
	Stars              int                `json:"stars,omitempty"`
	HistoricalCritique *ai.RecipeCritique `json:"historical_critique,omitempty"`
}

type evalDataset struct {
	SchemaVersion string       `json:"schema_version"`
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	CreatedAt     time.Time    `json:"created_at"`
	Samples       []evalSample `json:"samples"`
}

type evalResult struct {
	SchemaVersion     string             `json:"schema_version"`
	DatasetID         string             `json:"dataset_id"`
	PromptFingerprint string             `json:"prompt_fingerprint"`
	RequestedModel    string             `json:"requested_model"`
	RecipeHash        string             `json:"recipe_hash"`
	Critique          *ai.RecipeCritique `json:"critique"`
}

type evalStore struct {
	cache cache.ListCache
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: critiqueeval <snapshot|run|report|show> [flags]")
	}
	switch args[0] {
	case "snapshot":
		return runSnapshot(ctx, args[1:], out)
	case "run":
		return runModels(ctx, args[1:], out)
	case "report":
		return runReport(ctx, args[1:], out)
	case "show":
		return runShow(ctx, args[1:], out)
	default:
		return fmt.Errorf("unknown operation %q; want snapshot, run, report, or show", args[0])
	}
}

func runSnapshot(ctx context.Context, args []string, out io.Writer) error {
	var email, name, secretFile string
	var limit int
	fs := flag.NewFlagSet("critiqueeval snapshot", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&email, "email", "", "account email whose cooked recipes should be selected")
	fs.StringVar(&name, "name", "", "immutable dataset name")
	fs.IntVar(&limit, "n", defaultLimit, "maximum number of cooked recipes")
	fs.StringVar(&secretFile, "secret-file", "", "encrypted kage environment file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(email) == "" {
		return errors.New("-email is required")
	}
	if err := validateDatasetName(name); err != nil {
		return err
	}
	if limit < 1 {
		return errors.New("-n must be greater than zero")
	}
	c, cfg, err := configuredCache(secretFile)
	if err != nil {
		return err
	}
	_ = cfg
	dataset, err := buildDataset(ctx, c, email, name, limit, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := (evalStore{cache: c}).saveDataset(ctx, dataset); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Saved dataset %s (%s) with %d cooked recipes\n", dataset.Name, dataset.ID, len(dataset.Samples))
	return err
}

func runModels(ctx context.Context, args []string, out io.Writer) error {
	var datasetName, modelsCSV, secretFile string
	var refresh bool
	var workers int
	fs := flag.NewFlagSet("critiqueeval run", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&datasetName, "dataset", "", "named eval dataset")
	fs.StringVar(&modelsCSV, "models", "", "comma-separated OpenRouter model slugs")
	fs.StringVar(&secretFile, "secret-file", "", "encrypted kage environment file")
	fs.BoolVar(&refresh, "refresh", false, "rerun critiques already cached for this prompt and model")
	fs.IntVar(&workers, "workers", defaultWorkers, "maximum concurrent model calls")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateDatasetName(datasetName); err != nil {
		return fmt.Errorf("invalid -dataset: %w", err)
	}
	models := parseModels(modelsCSV)
	if len(models) == 0 {
		return errors.New("-models must contain at least one model slug")
	}
	if workers < 1 {
		return errors.New("-workers must be greater than zero")
	}
	c, cfg, err := configuredCache(secretFile)
	if err != nil {
		return err
	}
	store := evalStore{cache: c}
	dataset, err := store.loadDataset(ctx, datasetName)
	if err != nil {
		return err
	}

	var runErrs []error
	for _, model := range models {
		critiquer := ai.NewCritiquer(cfg.OpenRouter.APIKey, model, http.DefaultClient)
		if err := evaluateModel(ctx, store, dataset, model, critiquer, refresh, workers); err != nil {
			runErrs = append(runErrs, err)
		}
	}
	if err := printReport(ctx, out, store, dataset, models, ai.RecipeCritiqueFingerprint()); err != nil {
		runErrs = append(runErrs, err)
	}
	return errors.Join(runErrs...)
}

func runReport(ctx context.Context, args []string, out io.Writer) error {
	var datasetName, modelsCSV, secretFile, fingerprint string
	fs := flag.NewFlagSet("critiqueeval report", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&datasetName, "dataset", "", "named eval dataset")
	fs.StringVar(&modelsCSV, "models", "", "optional comma-separated model filter")
	fs.StringVar(&fingerprint, "prompt-fingerprint", ai.RecipeCritiqueFingerprint(), "critique prompt fingerprint to report")
	fs.StringVar(&secretFile, "secret-file", "", "encrypted kage environment file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateDatasetName(datasetName); err != nil {
		return fmt.Errorf("invalid -dataset: %w", err)
	}
	c, _, err := configuredCache(secretFile)
	if err != nil {
		return err
	}
	store := evalStore{cache: c}
	dataset, err := store.loadDataset(ctx, datasetName)
	if err != nil {
		return err
	}
	return printReport(ctx, out, store, dataset, parseModels(modelsCSV), strings.TrimSpace(fingerprint))
}

func runShow(ctx context.Context, args []string, out io.Writer) error {
	var datasetName, hash, modelsCSV, secretFile, fingerprint string
	fs := flag.NewFlagSet("critiqueeval show", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&datasetName, "dataset", "", "named eval dataset")
	fs.StringVar(&hash, "hash", "", "recipe hash")
	fs.StringVar(&modelsCSV, "models", "", "optional comma-separated model filter")
	fs.StringVar(&fingerprint, "prompt-fingerprint", ai.RecipeCritiqueFingerprint(), "critique prompt fingerprint to show")
	fs.StringVar(&secretFile, "secret-file", "", "encrypted kage environment file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateDatasetName(datasetName); err != nil {
		return fmt.Errorf("invalid -dataset: %w", err)
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return errors.New("-hash is required")
	}
	c, _, err := configuredCache(secretFile)
	if err != nil {
		return err
	}
	store := evalStore{cache: c}
	dataset, err := store.loadDataset(ctx, datasetName)
	if err != nil {
		return err
	}
	return printCritiqueDetails(ctx, out, store, dataset, hash, parseModels(modelsCSV), strings.TrimSpace(fingerprint))
}

func configuredCache(secretFile string) (cache.ListCache, *config.Config, error) {
	if secretFile = strings.TrimSpace(secretFile); secretFile != "" {
		if err := loadEncryptedEnvironment(secretFile); err != nil {
			return nil, nil, fmt.Errorf("load secret environment: %w", err)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	c, err := cache.MakeCache()
	if err != nil {
		return nil, nil, fmt.Errorf("create cache: %w", err)
	}
	return c, cfg, nil
}

func loadEncryptedEnvironment(path string) error {
	identities, err := kage.DefaultSSHIdentities()
	if err != nil {
		return fmt.Errorf("load SSH identity: %w", err)
	}
	if len(identities) == 0 {
		return errors.New("no SSH identity available")
	}
	secrets, err := kage.ReadEncryptedFile(path, identities)
	if err != nil {
		return err
	}
	for _, secret := range secrets {
		for _, line := range secret.Lines {
			if line.Key == "" {
				continue
			}
			if err := os.Setenv(line.Key, line.Value); err != nil {
				return fmt.Errorf("set %s: %w", line.Key, err)
			}
		}
	}
	return nil
}

func buildDataset(ctx context.Context, c cache.ListCache, email, name string, limit int, createdAt time.Time) (*evalDataset, error) {
	user, err := users.NewStorage(c).GetByEmail(email)
	if err != nil {
		return nil, fmt.Errorf("load user %q: %w", email, err)
	}
	feedbackIO := feedback.NewIO(c)
	type cookedRecipe struct {
		hash      string
		title     string
		updatedAt time.Time
		stars     int
	}
	cooked := make([]cookedRecipe, 0, len(user.LastRecipes))
	for _, saved := range user.LastRecipes {
		state, err := feedbackIO.FeedbackFromCache(ctx, saved.Hash)
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("load feedback for %s: %w", saved.Hash, err)
		}
		if !state.Cooked {
			continue
		}
		cooked = append(cooked, cookedRecipe{hash: saved.Hash, title: saved.Title, updatedAt: state.UpdatedAt, stars: state.Stars})
	}
	slices.SortFunc(cooked, func(a, b cookedRecipe) int {
		if cmp := b.updatedAt.Compare(a.updatedAt); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.hash, b.hash)
	})
	if len(cooked) > limit {
		cooked = cooked[:limit]
	}
	if len(cooked) == 0 {
		return nil, fmt.Errorf("user %q has no cooked recipes with cached feedback", email)
	}

	recipeStore := recipes.IO(c)
	critiqueStore := critique.NewStore(c)
	samples := make([]evalSample, 0, len(cooked))
	for _, item := range cooked {
		recipe, err := recipeStore.SingleFromCache(ctx, item.hash)
		if err != nil {
			return nil, fmt.Errorf("load cooked recipe %q (%s): %w", item.title, item.hash, err)
		}
		historical, err := critiqueStore.Load(ctx, item.hash)
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			return nil, fmt.Errorf("load historical critique for %s: %w", item.hash, err)
		}
		samples = append(samples, evalSample{
			Hash:               item.hash,
			Title:              item.title,
			Recipe:             *recipe,
			FeedbackUpdatedAt:  item.updatedAt,
			Stars:              item.stars,
			HistoricalCritique: historical,
		})
	}
	dataset := &evalDataset{SchemaVersion: "recipe-critique-eval-dataset-v1", Name: name, CreatedAt: createdAt, Samples: samples}
	dataset.ID, err = datasetID(samples)
	if err != nil {
		return nil, err
	}
	return dataset, nil
}

func datasetID(samples []evalSample) (string, error) {
	body, err := json.Marshal(samples)
	if err != nil {
		return "", fmt.Errorf("marshal eval dataset samples: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(body)), nil
}

func validateDatasetName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("dataset name is required")
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r) {
			continue
		}
		return fmt.Errorf("dataset name %q may contain only letters, digits, dot, underscore, and dash", name)
	}
	return nil
}

func parseModels(csv string) []string {
	seen := make(map[string]struct{})
	var models []string
	for _, value := range strings.Split(csv, ",") {
		model := strings.TrimSpace(value)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
}

func (s evalStore) saveDataset(ctx context.Context, dataset *evalDataset) error {
	body, err := json.Marshal(dataset)
	if err != nil {
		return fmt.Errorf("marshal eval dataset: %w", err)
	}
	if err := s.cache.Put(ctx, datasetPrefix+dataset.Name+".json", string(body), cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			return fmt.Errorf("eval dataset %q already exists; choose a new snapshot name", dataset.Name)
		}
		return fmt.Errorf("save eval dataset %q: %w", dataset.Name, err)
	}
	return nil
}

func (s evalStore) loadDataset(ctx context.Context, name string) (*evalDataset, error) {
	reader, err := s.cache.Get(ctx, datasetPrefix+name+".json")
	if err != nil {
		return nil, fmt.Errorf("load eval dataset %q: %w", name, err)
	}
	defer func() { _ = reader.Close() }()
	var dataset evalDataset
	if err := json.NewDecoder(reader).Decode(&dataset); err != nil {
		return nil, fmt.Errorf("decode eval dataset %q: %w", name, err)
	}
	return &dataset, nil
}

func modelHash(model string) string {
	sum := sha256.Sum256([]byte(model))
	return fmt.Sprintf("%x", sum[:8])
}

func resultKey(result evalResult) string {
	return fmt.Sprintf("%s%s/%s/%s/%s.json", resultPrefix, result.DatasetID, result.PromptFingerprint, modelHash(result.RequestedModel), result.RecipeHash)
}

func (s evalStore) saveResult(ctx context.Context, result evalResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.cache.Put(ctx, resultKey(result), string(body), cache.Unconditional())
}

func (s evalStore) loadResult(ctx context.Context, datasetID, fingerprint, model, hash string) (*evalResult, error) {
	key := resultKey(evalResult{DatasetID: datasetID, PromptFingerprint: fingerprint, RequestedModel: model, RecipeHash: hash})
	reader, err := s.cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	var result evalResult
	if err := json.NewDecoder(reader).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s evalStore) listResults(ctx context.Context, datasetID string) ([]evalResult, error) {
	keys, err := s.cache.List(ctx, resultPrefix+datasetID+"/", "")
	if err != nil {
		return nil, err
	}
	results := make([]evalResult, 0, len(keys))
	for _, suffix := range keys {
		reader, err := s.cache.Get(ctx, resultPrefix+datasetID+"/"+suffix)
		if err != nil {
			return nil, err
		}
		var result evalResult
		decodeErr := json.NewDecoder(reader).Decode(&result)
		_ = reader.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode eval result %s: %w", suffix, decodeErr)
		}
		results = append(results, result)
	}
	return results, nil
}

func evaluateModel(ctx context.Context, store evalStore, dataset *evalDataset, model string, critiquer recipeCritiquer, refresh bool, workers int) error {
	fingerprint := ai.RecipeCritiqueFingerprint()
	tasks := make(chan evalSample)
	errCh := make(chan error, len(dataset.Samples))
	var wg sync.WaitGroup
	for range min(workers, len(dataset.Samples)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sample := range tasks {
				if !refresh {
					_, err := store.loadResult(ctx, dataset.ID, fingerprint, model, sample.Hash)
					if err == nil {
						continue
					}
					if !errors.Is(err, cache.ErrNotFound) {
						errCh <- fmt.Errorf("load cached %s result for %s: %w", model, sample.Hash, err)
						continue
					}
				}
				value, err := critiquer.CritiqueRecipe(ctx, sample.Recipe)
				if err != nil {
					errCh <- fmt.Errorf("evaluate %s for %q (%s): %w", model, sample.Title, sample.Hash, err)
					continue
				}
				result := evalResult{SchemaVersion: "recipe-critique-eval-result-v1", DatasetID: dataset.ID, PromptFingerprint: fingerprint, RequestedModel: model, RecipeHash: sample.Hash, Critique: value}
				if err := store.saveResult(ctx, result); err != nil {
					errCh <- fmt.Errorf("save %s result for %s: %w", model, sample.Hash, err)
				}
			}
		}()
	}
	for _, sample := range dataset.Samples {
		tasks <- sample
	}
	close(tasks)
	wg.Wait()
	close(errCh)
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

type modelStats struct {
	count        int
	missing      int
	mean         float64
	variance     float64
	passRate     float64
	ratedCount   int
	mae          float64
	pearson      float64
	spearman     float64
	correlatable bool
}

func printReport(ctx context.Context, out io.Writer, store evalStore, dataset *evalDataset, modelFilter []string, fingerprint string) error {
	all, err := store.listResults(ctx, dataset.ID)
	if err != nil {
		return fmt.Errorf("list eval results: %w", err)
	}
	allowed := make(map[string]struct{}, len(modelFilter))
	byModel := make(map[string]map[string]evalResult)
	for _, model := range modelFilter {
		allowed[model] = struct{}{}
		byModel[model] = make(map[string]evalResult)
	}
	for _, result := range all {
		if fingerprint != "" && result.PromptFingerprint != fingerprint {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[result.RequestedModel]; !ok {
				continue
			}
		}
		if byModel[result.RequestedModel] == nil {
			byModel[result.RequestedModel] = make(map[string]evalResult)
		}
		byModel[result.RequestedModel][result.RecipeHash] = result
	}
	models := make([]string, 0, len(byModel))
	for model := range byModel {
		models = append(models, model)
	}
	sort.Strings(models)
	if _, err := fmt.Fprintf(out, "Dataset: %s (%s), %d cooked recipes\nPrompt fingerprint: %s\n", dataset.Name, dataset.ID, len(dataset.Samples), fingerprint); err != nil {
		return err
	}
	if len(models) == 0 {
		_, err := fmt.Fprintln(out, "No matching eval results")
		return err
	}
	if _, err := fmt.Fprint(out, "TITLE\tHASH\tFEEDBACK_AT\tSTARS\tHISTORICAL"); err != nil {
		return err
	}
	for _, model := range models {
		if _, err := fmt.Fprintf(out, "\t%s", model); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	for _, sample := range dataset.Samples {
		if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s", cleanCell(sample.Title), sample.Hash, sample.FeedbackUpdatedAt.Format(time.RFC3339), optionalInt(sample.Stars), critiqueCell(sample.HistoricalCritique)); err != nil {
			return err
		}
		for _, model := range models {
			result, ok := byModel[model][sample.Hash]
			cell := "-"
			if ok && result.Critique != nil {
				cell = fmt.Sprintf("%d/%s", result.Critique.OverallScore, passLabel(result.Critique.OverallScore))
			}
			if _, err := fmt.Fprintf(out, "\t%s", cell); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(out, "\nMODEL\tCOUNT\tMISSING\tMEAN\tVARIANCE\tPASS_RATE\tRATED\tSTAR_MAE\tPEARSON\tSPEARMAN"); err != nil {
		return err
	}
	for _, model := range models {
		stats := calculateModelStats(dataset.Samples, byModel[model])
		if _, err := fmt.Fprintf(out, "%s\t%d\t%d\t%.2f\t%.2f\t%.1f%%\t%d\t%s\t%s\t%s\n", model, stats.count, stats.missing, stats.mean, stats.variance, stats.passRate*100, stats.ratedCount, optionalFloat(stats.mae, stats.ratedCount > 0), optionalFloat(stats.pearson, stats.correlatable), optionalFloat(stats.spearman, stats.correlatable)); err != nil {
			return err
		}
	}
	return printPairwise(out, dataset.Samples, models, byModel)
}

func printCritiqueDetails(ctx context.Context, out io.Writer, store evalStore, dataset *evalDataset, hash string, modelFilter []string, fingerprint string) error {
	sampleIndex := slices.IndexFunc(dataset.Samples, func(sample evalSample) bool {
		return sample.Hash == hash
	})
	if sampleIndex < 0 {
		return fmt.Errorf("recipe %q is not in dataset %q", hash, dataset.Name)
	}
	sample := dataset.Samples[sampleIndex]
	if _, err := fmt.Fprintf(out, "%s (%s)\nFeedback: %s; stars: %s\n", sample.Title, sample.Hash, sample.FeedbackUpdatedAt.Format(time.RFC3339), optionalInt(sample.Stars)); err != nil {
		return err
	}
	if err := printCritiqueDetail(out, "Historical critique", sample.HistoricalCritique); err != nil {
		return err
	}

	allowed := make(map[string]struct{}, len(modelFilter))
	for _, model := range modelFilter {
		allowed[model] = struct{}{}
	}
	results, err := store.listResults(ctx, dataset.ID)
	if err != nil {
		return fmt.Errorf("list eval results: %w", err)
	}
	byModel := make(map[string]evalResult)
	for _, result := range results {
		if result.RecipeHash != hash || result.PromptFingerprint != fingerprint {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[result.RequestedModel]; !ok {
				continue
			}
		}
		byModel[result.RequestedModel] = result
	}
	models := make([]string, 0, len(byModel))
	for model := range byModel {
		models = append(models, model)
	}
	sort.Strings(models)
	if len(models) == 0 {
		_, err := fmt.Fprintf(out, "\nNo eval results for prompt fingerprint %s\n", fingerprint)
		return err
	}
	for _, model := range models {
		if err := printCritiqueDetail(out, "Eval critique requested from "+model, byModel[model].Critique); err != nil {
			return err
		}
	}
	return nil
}

func printCritiqueDetail(out io.Writer, heading string, value *ai.RecipeCritique) error {
	if _, err := fmt.Fprintf(out, "\n%s\n", heading); err != nil {
		return err
	}
	if value == nil {
		_, err := fmt.Fprintln(out, "- unavailable")
		return err
	}
	if _, err := fmt.Fprintf(out, "Model: %s\nScore: %d/%s\nSummary: %s\n", cleanCell(value.Model), value.OverallScore, passLabel(value.OverallScore), cleanCell(value.Summary)); err != nil {
		return err
	}
	if err := printDetailList(out, "Strengths", value.Strengths); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Issues:"); err != nil {
		return err
	}
	if len(value.Issues) == 0 {
		if _, err := fmt.Fprintln(out, "- none"); err != nil {
			return err
		}
	} else {
		for _, issue := range value.Issues {
			if _, err := fmt.Fprintf(out, "- [%s/%s] %s\n", cleanCell(issue.Category), cleanCell(issue.Severity), cleanCell(issue.Detail)); err != nil {
				return err
			}
		}
	}
	return printDetailList(out, "Suggested fixes", value.SuggestedFixes)
}

func printDetailList(out io.Writer, heading string, values []string) error {
	if _, err := fmt.Fprintf(out, "%s:\n", heading); err != nil {
		return err
	}
	if len(values) == 0 {
		_, err := fmt.Fprintln(out, "- none")
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(out, "- %s\n", cleanCell(value)); err != nil {
			return err
		}
	}
	return nil
}

func calculateModelStats(samples []evalSample, results map[string]evalResult) modelStats {
	stats := modelStats{missing: len(samples)}
	var scores, ratedScores, stars []float64
	passes := 0
	for _, sample := range samples {
		result, ok := results[sample.Hash]
		if !ok || result.Critique == nil {
			continue
		}
		score := float64(result.Critique.OverallScore)
		scores = append(scores, score)
		if score >= minimumPassScore {
			passes++
		}
		if sample.Stars > 0 {
			ratedScores = append(ratedScores, score)
			stars = append(stars, float64(sample.Stars))
			stats.mae += math.Abs(score/2 - float64(sample.Stars))
		}
	}
	stats.count = len(scores)
	stats.missing -= stats.count
	stats.ratedCount = len(stars)
	if stats.count > 0 {
		stats.mean = mean(scores)
		for _, score := range scores {
			delta := score - stats.mean
			stats.variance += delta * delta
		}
		stats.variance /= float64(stats.count)
		stats.passRate = float64(passes) / float64(stats.count)
	}
	if stats.ratedCount > 0 {
		stats.mae /= float64(stats.ratedCount)
	}
	if stats.ratedCount >= 2 && hasVariance(ratedScores) && hasVariance(stars) {
		stats.correlatable = true
		stats.pearson = pearson(ratedScores, stars)
		stats.spearman = pearson(ranks(ratedScores), ranks(stars))
	}
	return stats
}

func printPairwise(out io.Writer, samples []evalSample, models []string, byModel map[string]map[string]evalResult) error {
	if len(models) < 2 {
		return nil
	}
	if _, err := fmt.Fprintln(out, "\nPAIR\tBOTH\tMEAN_DELTA\tPASS_CHANGES\tMAX_ABS_DELTA"); err != nil {
		return err
	}
	for i := 0; i < len(models); i++ {
		for j := i + 1; j < len(models); j++ {
			var deltas []float64
			passChanges := 0
			maxDelta := 0
			for _, sample := range samples {
				a, aok := byModel[models[i]][sample.Hash]
				b, bok := byModel[models[j]][sample.Hash]
				if !aok || !bok || a.Critique == nil || b.Critique == nil {
					continue
				}
				delta := b.Critique.OverallScore - a.Critique.OverallScore
				deltas = append(deltas, float64(delta))
				if (a.Critique.OverallScore >= minimumPassScore) != (b.Critique.OverallScore >= minimumPassScore) {
					passChanges++
				}
				maxDelta = max(maxDelta, abs(delta))
			}
			if _, err := fmt.Fprintf(out, "%s -> %s\t%d\t%+.2f\t%d\t%d\n", models[i], models[j], len(deltas), mean(deltas), passChanges, maxDelta); err != nil {
				return err
			}
		}
	}
	return nil
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func hasVariance(values []float64) bool {
	if len(values) < 2 {
		return false
	}
	for _, value := range values[1:] {
		if value != values[0] {
			return true
		}
	}
	return false
}

func pearson(a, b []float64) float64 {
	ma, mb := mean(a), mean(b)
	var numerator, da, db float64
	for i := range a {
		x, y := a[i]-ma, b[i]-mb
		numerator += x * y
		da += x * x
		db += y * y
	}
	return numerator / math.Sqrt(da*db)
}

func ranks(values []float64) []float64 {
	type indexed struct {
		index int
		value float64
	}
	items := make([]indexed, len(values))
	for i, value := range values {
		items[i] = indexed{index: i, value: value}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].value < items[j].value })
	out := make([]float64, len(values))
	for start := 0; start < len(items); {
		end := start + 1
		for end < len(items) && items[end].value == items[start].value {
			end++
		}
		rank := (float64(start+1) + float64(end)) / 2
		for _, item := range items[start:end] {
			out[item.index] = rank
		}
		start = end
	}
	return out
}

func optionalInt(value int) string {
	if value == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", value)
}

func optionalFloat(value float64, available bool) string {
	if !available {
		return "-"
	}
	return fmt.Sprintf("%.3f", value)
}

func critiqueCell(value *ai.RecipeCritique) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%d/%s/%s", value.OverallScore, passLabel(value.OverallScore), cleanCell(value.Model))
}

func passLabel(score int) string {
	if score >= minimumPassScore {
		return "pass"
	}
	return "fail"
}

func cleanCell(value string) string {
	return strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(strings.TrimSpace(value))
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
