package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	"google.golang.org/genai"
)

const (
	recipeComparisonSchemaV1 = "recipe-comparison-v1"
)

const recipeComparisonSystemInstruction = `
You are a strict recipe editor comparing two generated recipes for a home cook.

Pick the recipe that is more useful, cookable, appealing, coherent, safe, and realistic as written.
Consider prep order, timing, ingredient use, flavor balance, cost plausibility, nutrition claims, and plating.
Do not prefer novelty alone. If both recipes are effectively equal, return tie.
Return JSON only.`

type RecipeComparisonWinner string

const (
	RecipeComparisonWinnerOriginal  RecipeComparisonWinner = "original"
	RecipeComparisonWinnerCandidate RecipeComparisonWinner = "candidate"
	RecipeComparisonWinnerTie       RecipeComparisonWinner = "tie"
)

type RecipeComparison struct {
	SchemaVersion  string                 `json:"schema_version" jsonschema:"enum=recipe-comparison-v1"`
	Winner         RecipeComparisonWinner `json:"winner" jsonschema:"enum=original,enum=candidate,enum=tie"`
	OriginalScore  int                    `json:"original_score" jsonschema:"minimum=1,maximum=10"`
	CandidateScore int                    `json:"candidate_score" jsonschema:"minimum=1,maximum=10"`
	Summary        string                 `json:"summary"`
	Reasons        []string               `json:"reasons"`
	Model          string                 `json:"model,omitempty" jsonschema:"-"`
	ComparedAt     time.Time              `json:"compared_at" jsonschema:"-"`
}

type recipeComparisonJudge struct {
	model  string
	schema map[string]any
	gem    *genai.Client
}

func NewRecipeComparisonJudge(apiKey, model string, httpClient *http.Client) *recipeComparisonJudge {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultGeminiCritiqueModel
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	client, err := genai.NewClient(context.TODO(), &genai.ClientConfig{
		APIKey:     apiKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: httpClient,
	})
	if err != nil {
		panic(err)
	}

	return &recipeComparisonJudge{
		gem:    client,
		model:  model,
		schema: recipeComparisonJSONSchema(),
	}
}

func (j *recipeComparisonJudge) CompareRecipes(ctx context.Context, original, candidate Recipe) (*RecipeComparison, error) {
	prompt, err := buildRecipeComparisonPrompt(original, candidate)
	if err != nil {
		return nil, fmt.Errorf("build recipe comparison prompt: %w", err)
	}

	start := time.Now()
	resp, err := j.gem.Models.GenerateContent(ctx, j.model, genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction:  genai.NewContentFromText(recipeComparisonSystemInstruction, genai.RoleUser),
		ResponseMIMEType:   "application/json",
		ResponseJsonSchema: j.schema,
	})
	if err != nil {
		return nil, fmt.Errorf("compare recipes: %w", err)
	}
	slog.InfoContext(ctx, "Gemini recipe comparison usage",
		"ai_category", aiCategoryCritique,
		"model", j.model,
		"model_version", resp.ModelVersion,
		"response_id", resp.ResponseID,
		"latencyMS", time.Since(start).Milliseconds(),
		geminiUsageLogAttr(j.model, resp.UsageMetadata),
	)

	comparison, err := parseRecipeComparison(resp.Text())
	if err != nil {
		return nil, err
	}
	comparison.Model = resp.ModelVersion
	comparison.ComparedAt = time.Now().UTC()
	return comparison, nil
}

func buildRecipeComparisonPrompt(original, candidate Recipe) (string, error) {
	originalPayload := original
	originalPayload.OriginHash = ""
	originalPayload.ParentHash = ""
	candidatePayload := candidate
	candidatePayload.OriginHash = ""
	candidatePayload.ParentHash = ""

	originalBody, err := json.MarshalIndent(originalPayload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal original recipe: %w", err)
	}
	candidateBody, err := json.MarshalIndent(candidatePayload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal candidate recipe: %w", err)
	}

	return fmt.Sprintf(
		"Compare these two generated recipes and choose the better one for a home cook.\nReturn JSON only using schema_version %q.\n\nOriginal recipe JSON:\n%s\n\nCandidate recipe JSON:\n%s",
		recipeComparisonSchemaV1,
		string(originalBody),
		string(candidateBody),
	), nil
}

func parseRecipeComparison(body string) (*RecipeComparison, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty recipe comparison response from Gemini")
	}
	var comparison RecipeComparison
	if err := json.Unmarshal([]byte(body), &comparison); err != nil {
		return nil, fmt.Errorf("failed to parse recipe comparison: %w", err)
	}
	comparison.SchemaVersion = recipeComparisonSchemaV1
	switch comparison.Winner {
	case RecipeComparisonWinnerOriginal, RecipeComparisonWinnerCandidate, RecipeComparisonWinnerTie:
	default:
		return nil, fmt.Errorf("recipe comparison winner must be original, candidate, or tie")
	}
	if comparison.OriginalScore < 1 || comparison.OriginalScore > 10 {
		return nil, fmt.Errorf("recipe comparison original score must be between 1 and 10")
	}
	if comparison.CandidateScore < 1 || comparison.CandidateScore > 10 {
		return nil, fmt.Errorf("recipe comparison candidate score must be between 1 and 10")
	}
	if strings.TrimSpace(comparison.Summary) == "" {
		return nil, fmt.Errorf("recipe comparison summary is required")
	}
	return &comparison, nil
}

func recipeComparisonJSONSchema() map[string]any {
	r := jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}
	schema := r.Reflect(&RecipeComparison{})
	body, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("marshal recipe comparison schema: %v", err))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		panic(fmt.Sprintf("decode recipe comparison schema: %v", err))
	}
	return out
}
