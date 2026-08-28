package ai

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const (
	openRouterBaseURL          = "https://openrouter.ai/api/v1"
	defaultCritiqueModel       = "anthropic/claude-opus-5"
	recipeCritiqueSchemaV1     = "recipe-critique-v1"
	openRouterApplicationTitle = "Careme"
	openRouterApplicationURL   = "https://careme.cooking"
)

const recipeCritiquePromptFormat = "Critique this generated recipe for correctness and usefulness to a home cook.\nReturn JSON only using schema_version %q.\nRecipe JSON:\n%s"

const recipeCritiqueSystemInstruction = `
You are a strict recipe editor reviewing AI-generated recipes before they are given to human cooks and used for future fine tuning.

Judge the recipe like an experienced chef helping create recipes to teach home cooks:
- is it realistic to cook as written
- are the instructions coherent and complete
- do the instructions begin with preparation before active cooking starts
- does each step cover one coherent task or component, keeping immediate actions on the same ingredient together and splitting unrelated work into separate steps
- when an ingredient is first used, does the instruction prose or a bullet include the exact amount used in that step, including pantry ingredients and ingredients divided among steps
- are bullet lists limited to preparations or mixtures of more than three ingredients, placed where those ingredients enter the action, and separated from surrounding prose by a blank line before and after the list
- do later steps refer concisely to named mixtures or prepared components without needlessly restating their constituent ingredients and amounts
- do the amounts used across instruction steps agree with each ingredient's total quantity in the ingredient list
- are the applications of salt, acid, fat, and heat appropriate
- when quantities permit calculation, use these salt amounts as starting points: 1.25% salt by weight for boneless meat, 1.5% for bone-in meat including roast chicken, 1% for vegetables and grains, and 2% salinity for pasta or vegetable-blanching water
- do not treat salt added later as a substitute for presalting meat or salting pasta or blanching water; salty ingredients added later may justify reducing finishing salt, but they do not correct food that was underseasoned during cooking
- account for ingredients that are already brined or cured and user requests to reduce sodium; because salt crystal sizes vary, evaluate salt by weight when available rather than assuming equal volume measures across salt types
- report a material deviation from these salt starting points as a flavor issue and suggest a corrected amount at the proper cooking stage; if it leaves a main component substantially underseasoned or oversalted, keep the overall score below 6 so the recipe is revised
- when doneness matters, does the recipe recommend the doneness that best suits the dish with one concise target or pull temperature and a brief rest when useful
- use this compact version of Careme's temperature guide as context (all temperatures are Fahrenheit): intact beef or lamb 125-130 for medium-rare and 135-140 for medium; pork loin or chops 140-145 and pork shoulder 195-205; ground beef, pork, veal, or lamb 160; all poultry 165 for safety, with breast pulled near 160 and rested to 165 and legs or thighs taken to 175-185 for a silkier texture; salmon 125-130 and lean white fish 135-140; egg dishes 160
- flag instructions to serve ground beef, pork, veal, or lamb below 160, poultry below 165 after any stated rest, or egg dishes below 160 as high-severity safety issues and keep the overall score below 6, unless the recipe gives a validated time-at-temperature method that achieves equivalent safety; suggest the corrected Careme target concisely
- do not use the preferred doneness ranges for intact beef, lamb, pork, or fish as automatic safety cutoffs; judge the full cooking method, intended doneness, time at temperature, and carryover cooking
- evaluate temperature instructions in the context of the full cooking method, including time at temperature, carryover cooking, and whether the food is an intact or ground cut; do not flag a temperature merely because it differs from a conventional or government-agency target
- does the recipe avoid naming the FDA, USDA, or other government agencies; quoting official food-safety guidance; comparing its recommendation with regulatory temperatures; or adding a temperature disclaimer; Careme links a separate temperature guide beside the recipe
- if temperature guidance is verbose, treat it as a clarity issue and suggest a concise cooking instruction that preserves the appropriate chef-recommended doneness
- do not name the FDA, USDA, or other government agencies, quote official temperature guidance, compare Careme's target with a regulatory target, or add a temperature disclaimer in any critique field
- are the total time, serving yield, total cost, and calories-per-serving estimates plausible for the ingredients and quantities
- does properties.total_minutes match the total time implied by all instruction steps, including prep, resting, and passive cooking
- do properties.cooking_methods match the instructions, avoid combining no_cook with another method
- is health empty unless it explains a meaningful dietary or nutritional ingredient swap and its practical tradeoff, without unsupported health claims
- does the dish sound balanced, appealing, and well plated
- are there any food safety or recipe logic issues

Be concise and concrete.
- overall_score must be an integer from 1 through 10
- summary must be a non-empty, concise sentence
- return one valid JSON object only
`

type RecipeCritiqueIssue struct {
	Severity string `json:"severity" jsonschema:"enum=low,enum=medium,enum=high"`
	Category string `json:"category" jsonschema:"enum=cookability,enum=safety,enum=clarity,enum=flavor,enum=timing,enum=cost,enum=nutrition,enum=ingredient_usage,enum=presentation"`
	Detail   string `json:"detail"`
}

type RecipeCritique struct {
	SchemaVersion string `json:"schema_version" jsonschema:"enum=recipe-critique-v1"`
	// OpenRouter routes Claude structured output through providers that reject
	// JSON Schema numeric bounds. parseRecipeCritique enforces the 1–10 range
	// after decoding instead.
	OverallScore int `json:"overall_score"`
	// creativity and practicality scores?
	Summary        string                `json:"summary"`
	Strengths      []string              `json:"strengths"`
	Issues         []RecipeCritiqueIssue `json:"issues"`
	SuggestedFixes []string              `json:"suggested_fixes"`
	Model          string                `json:"model,omitempty" jsonschema:"-"`
	CritiquedAt    time.Time             `json:"critiqued_at" jsonschema:"-"`
}

type critiquer struct {
	model  string
	schema map[string]any
	client openai.Client
}

func NewCritiquer(apiKey, model string, httpClient *http.Client) *critiquer {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultCritiqueModel
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(openRouterBaseURL),
		option.WithHeader("HTTP-Referer", openRouterApplicationURL),
		option.WithHeader("X-OpenRouter-Title", openRouterApplicationTitle),
	}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}

	return &critiquer{
		client: openai.NewClient(opts...),
		model:  model,
		schema: recipeCritiqueJSONSchema(),
	}
}

func (c *critiquer) Ready(ctx context.Context) error {
	if err := c.client.Get(ctx, "key", nil, nil); err != nil {
		return fmt.Errorf("check OpenRouter API key: %w", err)
	}
	return nil
}

func (c *critiquer) CritiqueRecipe(ctx context.Context, recipe Recipe) (*RecipeCritique, error) {
	prompt, err := buildRecipeCritiquePrompt(recipe)
	if err != nil {
		return nil, fmt.Errorf("failed to build recipe critique prompt: %w", err)
	}

	start := time.Now()
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: c.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(recipeCritiqueSystemInstruction),
			openai.UserMessage(prompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   "recipe_critique",
					Schema: c.schema,
					Strict: openai.Bool(true),
				},
			},
		},
	}, option.WithJSONSet("provider.require_parameters", true))
	if err != nil {
		return nil, fmt.Errorf("failed to critique recipe: %w", err)
	}
	slog.InfoContext(ctx, "OpenRouter critique usage",
		"ai_category", aiCategoryCritique,
		"model", c.model,
		"response_model", resp.Model,
		"response_id", resp.ID,
		"latencyMS", time.Since(start).Milliseconds(),
		openRouterUsageLogAttr(resp),
	)

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty response from OpenRouter critique model")
	}
	critique, err := parseRecipeCritique(resp.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	critique.Model = strings.TrimSpace(resp.Model)
	if critique.Model == "" {
		critique.Model = c.model
	}
	critique.CritiquedAt = time.Now().UTC()
	return critique, nil
}

func openRouterUsageLogAttr(resp *openai.ChatCompletion) slog.Attr {
	if resp == nil || !resp.JSON.Usage.Valid() {
		return slog.Group("usage", slog.Bool("available", false))
	}
	attrs := []any{
		slog.Bool("available", true),
		slog.Int64("promptTokenCount", resp.Usage.PromptTokens),
		slog.Int64("cachedTokenCount", resp.Usage.PromptTokensDetails.CachedTokens),
		slog.Int64("completionTokenCount", resp.Usage.CompletionTokens),
		slog.Int64("reasoningTokenCount", resp.Usage.CompletionTokensDetails.ReasoningTokens),
		slog.Int64("totalTokenCount", resp.Usage.TotalTokens),
	}
	if cost, ok := openRouterResponseCost(resp.RawJSON()); ok {
		attrs = append(attrs, slog.Float64("costCredits", cost))
	}

	return slog.Group("usage", attrs...)
}

func openRouterResponseCost(body string) (float64, bool) {
	var response struct {
		Usage struct {
			Cost *float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(body), &response); err != nil || response.Usage.Cost == nil {
		return 0, false
	}
	return *response.Usage.Cost, true
}

func parseRecipeCritique(body string) (*RecipeCritique, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty critique response from OpenRouter")
	}
	var critique RecipeCritique
	if err := json.Unmarshal([]byte(body), &critique); err != nil {
		return nil, fmt.Errorf("failed to parse OpenRouter critique: %w", err)
	}
	critique.SchemaVersion = recipeCritiqueSchemaV1

	if critique.Summary == "" {
		return nil, fmt.Errorf("OpenRouter critique summary is required")
	}
	if critique.OverallScore < 1 || critique.OverallScore > 10 {
		return nil, fmt.Errorf("OpenRouter critique overall score must be between 1 and 10")
	}
	return &critique, nil
}

func buildRecipeCritiquePrompt(recipe Recipe) (string, error) {
	payload := recipe
	payload.OriginHash = ""
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal recipe critique payload: %w", err)
	}
	return fmt.Sprintf(
		recipeCritiquePromptFormat,
		recipeCritiqueSchemaV1,
		string(body),
	), nil
}

// RecipeCritiqueFingerprint identifies the instructions and output schema used
// for a critique so eval caches cannot cross prompt or schema revisions.
func RecipeCritiqueFingerprint() string {
	body, err := json.Marshal(recipeCritiqueJSONSchema())
	if err != nil {
		panic(fmt.Sprintf("marshal recipe critique fingerprint schema: %v", err))
	}
	value := strings.Join([]string{
		recipeCritiqueSystemInstruction,
		recipeCritiquePromptFormat,
		recipeCritiqueSchemaV1,
		string(body),
	}, "\n")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func recipeCritiqueJSONSchema() map[string]any {
	r := jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}
	schema := r.Reflect(&RecipeCritique{})
	body, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("marshal recipe critique schema: %v", err))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		panic(fmt.Sprintf("decode recipe critique schema: %v", err))
	}
	return out
}
