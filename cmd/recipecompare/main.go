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
	"path/filepath"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/recipes"
	"careme/internal/recipes/prompts"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const (
	defaultTargetModel = "gpt-5.6-sol"
	comparisonPrefix   = "recipe_model_comparisons/"
)

type promptReplayer interface {
	ReplayRecipePrompt(ctx context.Context, record *ai.PromptRecord) (*ai.Recipe, error)
}

type recipeJudge interface {
	CompareRecipes(ctx context.Context, original, candidate ai.Recipe) (*ai.RecipeComparison, error)
}

type recipeCandidate struct {
	SourceKind string
	SourceHash string
	Hash       string
	Recipe     ai.Recipe
}

type comparisonArtifact struct {
	SourceKind       string               `json:"source_kind"`
	SourceHash       string               `json:"source_hash"`
	OriginalHash     string               `json:"original_hash"`
	OriginalTitle    string               `json:"original_title"`
	OriginalResponse string               `json:"original_response_id"`
	TargetModel      string               `json:"target_model"`
	CandidateHash    string               `json:"candidate_hash"`
	CandidateRecipe  ai.Recipe            `json:"candidate_recipe"`
	Comparison       *ai.RecipeComparison `json:"comparison"`
	CreatedAt        time.Time            `json:"created_at"`
}

type comparisonRow struct {
	SourceKind string
	SourceHash string
	Hash       string
	Original   ai.Recipe
	Artifact   *comparisonArtifact
	Skipped    string
	Err        error
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*f = append(*f, part)
		}
	}
	return nil
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	var shoppingHashes stringListFlag
	var recipeHashes stringListFlag
	var model string
	var critiqueModel string
	var secretFile string
	var refresh bool

	fs := flag.NewFlagSet("recipecompare", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Var(&shoppingHashes, "shopping-hash", "shopping list hash to compare; may be repeated or comma-separated")
	fs.Var(&recipeHashes, "recipe-hash", "recipe hash to compare; may be repeated or comma-separated")
	fs.StringVar(&model, "model", defaultTargetModel, "target OpenAI recipe model for regenerated candidates")
	fs.StringVar(&critiqueModel, "critique-model", "", "Gemini model to use for pairwise judging")
	fs.StringVar(&secretFile, "secret-file", "", "encrypted dotenv secrets file to load before config; values override existing env")
	fs.BoolVar(&refresh, "refresh", false, "rerun replay and judging even if cached comparison exists")
	if err := fs.Parse(args); err != nil {
		return err
	}

	shoppingHashes = normalizeHashInputs(shoppingHashes)
	recipeHashes = normalizeHashInputs(recipeHashes)
	if len(shoppingHashes) == 0 && len(recipeHashes) == 0 {
		return errors.New("must provide at least one -shopping-hash or -recipe-hash")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("-model is required")
	}
	if secretFile = strings.TrimSpace(secretFile); secretFile != "" {
		if err := config.LoadEncryptedEnvOverride(secretFile); err != nil {
			return fmt.Errorf("load secret file %q: %w", secretFile, err)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cacheStore, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	replayer := ai.NewClient(cfg.AI.APIKey, model, httpClient, prompts.NewCacheRecorder(cacheStore))
	judge := ai.NewRecipeComparisonJudge(cfg.Gemini.APIKey, critiqueModel, httpClient)

	rows, err := compareInputs(ctx, cacheStore, replayer, judge, model, shoppingHashes, recipeHashes, refresh)
	if err != nil {
		return err
	}
	return printRows(out, rows)
}

func normalizeHashInputs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(strings.TrimSpace(value))
		value = strings.TrimPrefix(value, "./")
		value = strings.TrimPrefix(value, "/")
		value = strings.TrimPrefix(value, recipes.ShoppingListCachePrefix)
		value = strings.TrimPrefix(value, "recipe/")
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func compareInputs(
	ctx context.Context,
	cacheStore cache.ListCache,
	replayer promptReplayer,
	judge recipeJudge,
	model string,
	shoppingHashes []string,
	recipeHashes []string,
	refresh bool,
) ([]comparisonRow, error) {
	candidates, err := loadCandidates(ctx, cacheStore, shoppingHashes, recipeHashes)
	if err != nil {
		return nil, err
	}
	store := comparisonStore{cache: cacheStore, model: model}

	rows := make([]comparisonRow, 0, len(candidates))
	for _, candidate := range candidates {
		row := compareCandidate(ctx, cacheStore, store, replayer, judge, candidate, model, refresh)
		rows = append(rows, row)
	}
	return rows, nil
}

func loadCandidates(ctx context.Context, cacheStore cache.ListCache, shoppingHashes []string, recipeHashes []string) ([]recipeCandidate, error) {
	rio := recipes.IO(cacheStore)
	candidates := make([]recipeCandidate, 0)
	seen := make(map[string]struct{})

	add := func(sourceKind, sourceHash string, recipe ai.Recipe) {
		hash := recipe.ComputeHash()
		if _, ok := seen[hash]; ok {
			return
		}
		seen[hash] = struct{}{}
		candidates = append(candidates, recipeCandidate{
			SourceKind: sourceKind,
			SourceHash: sourceHash,
			Hash:       hash,
			Recipe:     recipe,
		})
	}

	for _, hash := range shoppingHashes {
		list, err := rio.FromCache(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("load shopping list %s: %w", hash, err)
		}
		for _, recipe := range list.Recipes {
			add("shoppinglist", hash, recipe)
		}
	}

	for _, hash := range recipeHashes {
		recipe, err := rio.SingleFromCache(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("load recipe %s: %w", hash, err)
		}
		add("recipe", hash, *recipe)
	}

	return candidates, nil
}

func compareCandidate(
	ctx context.Context,
	cacheStore cache.ListCache,
	store comparisonStore,
	replayer promptReplayer,
	judge recipeJudge,
	candidate recipeCandidate,
	model string,
	refresh bool,
) comparisonRow {
	row := comparisonRow{
		SourceKind: candidate.SourceKind,
		SourceHash: candidate.SourceHash,
		Hash:       candidate.Hash,
		Original:   candidate.Recipe,
	}

	if !refresh {
		artifact, err := store.Load(ctx, candidate.Hash)
		if err == nil {
			row.Artifact = artifact
			return row
		}
		if !errors.Is(err, cache.ErrNotFound) {
			row.Err = fmt.Errorf("load cached comparison: %w", err)
			return row
		}
	}

	responseID := strings.TrimSpace(candidate.Recipe.ResponseID)
	if responseID == "" {
		row.Skipped = "missing recipe response ID"
		return row
	}
	record, err := promptRecordWithParentInputsFromCache(ctx, cacheStore, responseID)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			row.Skipped = "missing recipe prompt record"
			return row
		}
		row.Err = fmt.Errorf("load prompt record: %w", err)
		return row
	}

	replayed, err := replayer.ReplayRecipePrompt(ctx, record)
	if err != nil {
		row.Err = fmt.Errorf("replay recipe prompt: %w", err)
		return row
	}
	if replayed == nil {
		row.Err = errors.New("replay recipe prompt returned no recipe")
		return row
	}
	comparison, err := judge.CompareRecipes(ctx, candidate.Recipe, *replayed)
	if err != nil {
		row.Err = fmt.Errorf("judge recipes: %w", err)
		return row
	}

	artifact := &comparisonArtifact{
		SourceKind:       candidate.SourceKind,
		SourceHash:       candidate.SourceHash,
		OriginalHash:     candidate.Hash,
		OriginalTitle:    candidate.Recipe.Title,
		OriginalResponse: responseID,
		TargetModel:      model,
		CandidateHash:    replayed.ComputeHash(),
		CandidateRecipe:  *replayed,
		Comparison:       comparison,
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.Save(ctx, candidate.Hash, artifact); err != nil {
		row.Err = fmt.Errorf("save comparison: %w", err)
		return row
	}
	row.Artifact = artifact
	return row
}

func promptRecordWithParentInputsFromCache(ctx context.Context, c cache.Cache, responseID string) (*ai.PromptRecord, error) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return nil, cache.ErrNotFound
	}

	record, err := promptRecordFromCache(ctx, c, responseID)
	if err != nil {
		return nil, err
	}

	parentResponseID := strings.TrimSpace(record.PreviousResponseID)
	if parentResponseID == "" {
		record.Input = append([]ai.PromptMessage(nil), record.Input...)
		return record, nil
	}
	parent, err := promptRecordWithParentInputsFromCache(ctx, c, parentResponseID)
	if err != nil {
		return nil, err
	}
	record.Input = append(parent.Input, record.Input...)
	return record, nil
}

func promptRecordFromCache(ctx context.Context, c cache.Cache, responseID string) (*ai.PromptRecord, error) {
	promptReader, err := c.Get(ctx, prompts.CachePrefix+responseID)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = promptReader.Close()
	}()

	var record ai.PromptRecord
	if err := json.NewDecoder(promptReader).Decode(&record); err != nil {
		return nil, fmt.Errorf("decode prompt record: %w", err)
	}
	return &record, nil
}

type comparisonStore struct {
	cache cache.ListCache
	model string
}

func (s comparisonStore) key(hash string) string {
	model := strings.NewReplacer("/", "_", "\\", "_").Replace(strings.TrimSpace(s.model))
	return comparisonPrefix + model + "/" + hash
}

func (s comparisonStore) Load(ctx context.Context, hash string) (*comparisonArtifact, error) {
	reader, err := s.cache.Get(ctx, s.key(hash))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var artifact comparisonArtifact
	if err := json.NewDecoder(reader).Decode(&artifact); err != nil {
		return nil, fmt.Errorf("decode comparison artifact: %w", err)
	}
	return &artifact, nil
}

func (s comparisonStore) Save(ctx context.Context, hash string, artifact *comparisonArtifact) error {
	if artifact == nil {
		return errors.New("comparison artifact is required")
	}
	body, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("marshal comparison artifact: %w", err)
	}
	return s.cache.Put(ctx, s.key(hash), string(body), cache.Unconditional())
}

func printRows(out io.Writer, rows []comparisonRow) error {
	stats := comparisonStats(rows)
	if _, err := fmt.Fprintf(out, "Compared %d recipes; skipped=%d errors=%d original_wins=%d candidate_wins=%d ties=%d\n",
		stats.Compared,
		stats.Skipped,
		stats.Errors,
		stats.OriginalWins,
		stats.CandidateWins,
		stats.Ties,
	); err != nil {
		return err
	}

	for _, row := range rows {
		if row.Skipped != "" {
			if _, err := fmt.Fprintf(out, "SKIP\t%s\t%s\t%s\t%s\n", row.SourceKind, row.SourceHash, row.Hash, row.Skipped); err != nil {
				return err
			}
			continue
		}
		if row.Err != nil {
			if _, err := fmt.Fprintf(out, "ERROR\t%s\t%s\t%s\t%v\n", row.SourceKind, row.SourceHash, row.Hash, row.Err); err != nil {
				return err
			}
			continue
		}
		if row.Artifact == nil || row.Artifact.Comparison == nil {
			if _, err := fmt.Fprintf(out, "ERROR\t%s\t%s\t%s\tmissing comparison result\n", row.SourceKind, row.SourceHash, row.Hash); err != nil {
				return err
			}
			continue
		}
		comparison := row.Artifact.Comparison
		if _, err := fmt.Fprintf(out, "OK\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.SourceKind,
			row.SourceHash,
			row.Hash,
			row.Original.Title,
			row.Artifact.CandidateRecipe.Title,
			comparison.Winner,
			strings.TrimSpace(comparison.Summary),
		); err != nil {
			return err
		}
	}
	return nil
}

type stats struct {
	Compared      int
	Skipped       int
	Errors        int
	OriginalWins  int
	CandidateWins int
	Ties          int
}

func comparisonStats(rows []comparisonRow) stats {
	var s stats
	for _, row := range rows {
		if row.Skipped != "" {
			s.Skipped++
			continue
		}
		if row.Err != nil || row.Artifact == nil || row.Artifact.Comparison == nil {
			s.Errors++
			continue
		}
		s.Compared++
		switch row.Artifact.Comparison.Winner {
		case ai.RecipeComparisonWinnerOriginal:
			s.OriginalWins++
		case ai.RecipeComparisonWinnerCandidate:
			s.CandidateWins++
		case ai.RecipeComparisonWinnerTie:
			s.Ties++
		}
	}
	return s
}
