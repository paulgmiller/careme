package gradereview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"careme/internal/cache"

	"gopkg.in/yaml.v3"
)

type EvalOptions struct {
	LocationID   string
	CacheVersion string
}

type evalTest struct {
	Description string         `yaml:"description"`
	Vars        evalVars       `yaml:"vars"`
	Metadata    map[string]any `yaml:"metadata"`
}
type evalVars struct {
	Cases []evalCase `yaml:"cases"`
}
type evalCase struct {
	Ingredient map[string]any `yaml:"ingredient"`
	Expect     scoreBounds    `yaml:"expect"`
}
type scoreBounds struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

// WriteEvalCases exports a reproducible snapshot using the ingredient eval's
// existing vars.cases interface. No model calls are needed to export reviews.
func WriteEvalCases(ctx context.Context, out io.Writer, c cache.ListCache, options EvalOptions) error {
	prefix := reviewCachePrefix
	if options.CacheVersion != "" {
		prefix += options.CacheVersion + "/"
	}
	keys, err := c.List(ctx, prefix, "")
	if err != nil {
		return fmt.Errorf("list ingredient reviews: %w", err)
	}
	sort.Strings(keys)
	tests := make([]evalTest, 0, len(keys))
	for _, suffix := range keys {
		key := prefix + suffix
		review, err := readReview(ctx, c, key)
		if err != nil {
			return err
		}
		if options.LocationID != "" && review.LocationID != options.LocationID {
			continue
		}
		test, err := reviewEvalCase(review)
		if err != nil {
			return fmt.Errorf("export review %q: %w", key, err)
		}
		tests = append(tests, test)
	}
	if len(tests) == 0 {
		return fmt.Errorf("no ingredient reviews match the selected filters")
	}
	enc := yaml.NewEncoder(out)
	enc.SetIndent(2)
	if err := enc.Encode(tests); err != nil {
		return fmt.Errorf("encode ingredient eval cases: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("finish ingredient eval cases: %w", err)
	}
	return nil
}

func readReview(ctx context.Context, c cache.Cache, key string) (Review, error) {
	reader, err := c.Get(ctx, key)
	if err != nil {
		return Review{}, fmt.Errorf("load ingredient review %q: %w", key, err)
	}
	defer func() { _ = reader.Close() }()
	var review Review
	if err := json.NewDecoder(reader).Decode(&review); err != nil {
		return Review{}, fmt.Errorf("decode ingredient review %q: %w", key, err)
	}
	if review.GradeKey == "" || reviewCachePrefix+review.GradeKey != key || !review.Verdict.Valid() || review.Ingredient.Grade == nil || review.ReviewedAt.IsZero() {
		return Review{}, fmt.Errorf("invalid ingredient review %q", key)
	}
	if review.Ingredient.Grade.Score < 0 || review.Ingredient.Grade.Score > 10 {
		return Review{}, fmt.Errorf("invalid reviewed score in %q", key)
	}
	return review, nil
}

func reviewEvalCase(review Review) (evalTest, error) {
	score := review.Ingredient.Grade.Score
	bounds := scoreBounds{Min: 0, Max: 10}
	switch review.Verdict {
	case VerdictTooHigh:
		bounds.Max = score
	case VerdictCorrect:
		bounds.Min = max(0, score-1)
		bounds.Max = min(10, score+1)
	case VerdictTooLow:
		bounds.Min = score
	}
	ingredient := review.Ingredient
	ingredient.Grade = nil
	ingredient.Embedding = nil
	body, err := json.Marshal(ingredient)
	if err != nil {
		return evalTest{}, err
	}
	var input map[string]any
	if err := json.Unmarshal(body, &input); err != nil {
		return evalTest{}, err
	}
	description := strings.TrimSpace(ingredient.Description)
	if description == "" {
		description = ingredient.ProductID
	}
	return evalTest{
		Description: fmt.Sprintf("Human review: %s (%s, score %d, %s)", description, review.Verdict, score, review.GradeKey),
		Vars:        evalVars{Cases: []evalCase{{Ingredient: input, Expect: bounds}}},
		Metadata:    map[string]any{"grade_key": review.GradeKey, "location_id": review.LocationID, "verdict": string(review.Verdict), "reviewed_score": score, "reviewed_at": review.ReviewedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")},
	}, nil
}
