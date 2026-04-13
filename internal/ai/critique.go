package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	"google.golang.org/genai"
)

const (
	// https://ai.google.dev/gemini-api/docs/models
	defaultGeminiCritiqueModel = "gemini-3.1-pro-preview" //"gemini-2.5-flash"
	recipeCritiqueSchemaV1     = "recipe-critique-v1"
)

const recipeCritiqueSystemInstruction = `
You are a strict recipe editor reviewing AI-generated recipes before they are given to human cooks and used for future fine tuning.

Judge the recipe like an experienced chef helping create recipes to teach home cooks:
- is it realistic to cook as written
- are the instructions coherent and complete
- are the applications of salt, acid, fat, and heat appropriate
- are the timing and cost estimates plausible
- does the dish sound balanced, appealing, and well plated
- are there any food safety or recipe logic issues

Be concise and concrete. Return JSON only.`

type RecipeCritiqueIssue struct {
	Severity string `json:"severity" jsonschema:"enum=low,enum=medium,enum=high"`
	Category string `json:"category" jsonschema:"enum=cookability,enum=safety,enum=clarity,enum=flavor,enum=timing,enum=cost,enum=nutrition,enum=ingredient_usage,enum=presentation"`
	Detail   string `json:"detail"`
}

type RecipeCritique struct {
	SchemaVersion string `json:"schema_version" jsonschema:"enum=recipe-critique-v1"`
	OverallScore  int    `json:"overall_score" jsonschema:"minimum=1,maximum=10"`
	// creativity and practicality scores?
	Summary        string                `json:"summary"`
	Strengths      []string              `json:"strengths"`
	Issues         []RecipeCritiqueIssue `json:"issues"`
	SuggestedFixes []string              `json:"suggested_fixes"`
	Model          string                `json:"model,omitempty" jsonschema:"-"`
	CritiquedAt    time.Time             `json:"critiqued_at,omitempty" jsonschema:"-"`
}

type critiquer struct {
	apiKey string
	model  string
	schema map[string]any
}

func NewCritiquer(apiKey, model string) *critiquer {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultGeminiCritiqueModel
	}
	return &critiquer{
		apiKey: strings.TrimSpace(apiKey),
		model:  model,
		schema: recipeCritiqueJSONSchema(),
	}
}

func (c *critiquer) Ready(ctx context.Context) error {
	client, err := c.newClient(ctx)
	if err != nil {
		return err
	}
	for _, err := range client.Models.All(ctx) {
		return err
	}
	return fmt.Errorf("model not found: %s", c.model)
	/* expensive?
	resp, err := client.Models.GenerateContent(ctx, c.model, genai.Text("Reply with ready."), &genai.GenerateContentConfig{
		Temperature:     genai.Ptr[float32](0),
		MaxOutputTokens: 8,
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(resp.Text()) == "" {
		return fmt.Errorf("empty response from Gemini critique model")
	}
	*/
}

func (c *critiquer) CritiqueRecipe(ctx context.Context, recipe Recipe) (*RecipeCritique, error) {
	prompt, err := buildRecipeCritiquePrompt(recipe)
	if err != nil {
		return nil, fmt.Errorf("failed to build recipe critique prompt: %w", err)
	}
	client, err := c.newClient(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := client.Models.GenerateContent(ctx, c.model, genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: genai.NewContentFromText(recipeCritiqueSystemInstruction, genai.RoleUser),
		// Temperature:        genai.Ptr[float32](0),
		// MaxOutputTokens:    768,
		ResponseMIMEType:   "application/json",
		ResponseJsonSchema: c.schema,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to critique recipe: %w", err)
	}
	slog.InfoContext(ctx, "Gemini critique usage",
		"model", c.model,
		"model_version", resp.ModelVersion,
		"response_id", resp.ResponseID,
		"usage", resp.UsageMetadata,
	)

	critique, err := parseRecipeCritique(resp.Text())
	if err != nil {
		return nil, err
	}
	critique.Model = resp.ModelVersion
	critique.CritiquedAt = time.Now().UTC()
	return critique, nil
}

func (c *critiquer) newClient(ctx context.Context) (*genai.Client, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  c.apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create Gemini client: %w", err)
	}
	return client, nil
}

func parseRecipeCritique(body string) (*RecipeCritique, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("empty critique response from Gemini")
	}
	var critique RecipeCritique
	if err := json.Unmarshal([]byte(body), &critique); err != nil {
		return nil, fmt.Errorf("failed to parse Gemini critique: %w", err)
	}
	critique.SchemaVersion = recipeCritiqueSchemaV1

	if critique.Summary == "" {
		return nil, fmt.Errorf("gemini critique summary is required")
	}
	if critique.OverallScore < 1 || critique.OverallScore > 10 {
		return nil, fmt.Errorf("gemini critique overall score must be between 1 and 10")
	}
	return &critique, nil
}

func buildRecipeCritiquePrompt(recipe Recipe) (string, error) {
	payload := recipe
	payload.OriginHash = ""
	payload.Saved = false
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal recipe critique payload: %w", err)
	}
	return fmt.Sprintf(
		"Critique this generated recipe for correctness and usefulness to a home cook.\nReturn JSON only using schema_version %q.\nRecipe JSON:\n%s",
		recipeCritiqueSchemaV1,
		string(body),
	), nil
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
