package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/genai"
)

const (
	defaultGeminiCritiqueModel = "gemini-2.5-flash"
	recipeCritiqueSchemaV1     = "recipe-critique-v1"
)

const recipeCritiqueSystemInstruction = `
You are a strict recipe editor reviewing AI-generated recipes before they are reused for future product tuning.

Judge the recipe like an experienced chef teaching home cooks:
- is it realistic to cook as written
- are the instructions coherent and complete
- are the timing and cost estimates plausible
- does the dish sound balanced, appealing, and well plated
- are there any food safety or recipe logic issues

Be concise and concrete. Return JSON only.`

type RecipeCritiqueIssue struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Detail   string `json:"detail"`
}

type RecipeCritique struct {
	SchemaVersion  string                `json:"schema_version"`
	OverallScore   int                   `json:"overall_score"`
	Summary        string                `json:"summary"`
	Strengths      []string              `json:"strengths"`
	Issues         []RecipeCritiqueIssue `json:"issues"`
	SuggestedFixes []string              `json:"suggested_fixes"`
	Model          string                `json:"model,omitempty"`
	CritiquedAt    time.Time             `json:"critiqued_at,omitempty"`
}

type Critiquer struct {
	apiKey string
	model  string
	schema *genai.Schema
}

func NewCritiquer(apiKey, model string) *Critiquer {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultGeminiCritiqueModel
	}
	return &Critiquer{
		apiKey: strings.TrimSpace(apiKey),
		model:  model,
		schema: recipeCritiqueSchema(),
	}
}

func (c *Critiquer) Ready(ctx context.Context) error {
	if c == nil || c.apiKey == "" {
		return fmt.Errorf("gemini critique client is not configured")
	}
	client, err := c.newClient(ctx)
	if err != nil {
		return err
	}
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
	return nil
}

func (c *Critiquer) CritiqueRecipe(ctx context.Context, recipe Recipe) (*RecipeCritique, error) {
	if c == nil || c.apiKey == "" {
		return nil, fmt.Errorf("gemini critique client is not configured")
	}
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
		Temperature:       genai.Ptr[float32](0),
		MaxOutputTokens:   768,
		ResponseMIMEType:  "application/json",
		ResponseSchema:    c.schema,
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
	critique.Model = firstNonEmpty(strings.TrimSpace(resp.ModelVersion), c.model)
	if critique.CritiquedAt.IsZero() {
		critique.CritiquedAt = time.Now().UTC()
	}
	return critique, nil
}

func (c *Critiquer) newClient(ctx context.Context) (*genai.Client, error) {
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
	critique.SchemaVersion = firstNonEmpty(strings.TrimSpace(critique.SchemaVersion), recipeCritiqueSchemaV1)
	critique.Summary = strings.TrimSpace(critique.Summary)
	critique.Strengths = compactTrimmed(critique.Strengths)
	critique.SuggestedFixes = compactTrimmed(critique.SuggestedFixes)
	for i := range critique.Issues {
		critique.Issues[i].Severity = strings.TrimSpace(strings.ToLower(critique.Issues[i].Severity))
		critique.Issues[i].Category = strings.TrimSpace(strings.ToLower(critique.Issues[i].Category))
		critique.Issues[i].Detail = strings.TrimSpace(critique.Issues[i].Detail)
	}
	critique.Issues = compactIssues(critique.Issues)

	if critique.Summary == "" {
		return nil, fmt.Errorf("gemini critique summary is required")
	}
	if critique.OverallScore < 1 || critique.OverallScore > 10 {
		return nil, fmt.Errorf("gemini critique overall score must be between 1 and 10")
	}
	return &critique, nil
}

func buildRecipeCritiquePrompt(recipe Recipe) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Critique this generated recipe for correctness and usefulness to a home cook.\n\n")
	fmt.Fprintf(&b, "Recipe:\n")
	fmt.Fprintf(&b, "Title: %s\n", recipe.Title)
	if recipe.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", recipe.Description)
	}
	if recipe.CookTime != "" {
		fmt.Fprintf(&b, "Cook time: %s\n", recipe.CookTime)
	}
	if recipe.CostEstimate != "" {
		fmt.Fprintf(&b, "Cost estimate: %s\n", recipe.CostEstimate)
	}
	if recipe.Health != "" {
		fmt.Fprintf(&b, "Health notes: %s\n", recipe.Health)
	}
	if recipe.DrinkPairing != "" {
		fmt.Fprintf(&b, "Drink pairing: %s\n", recipe.DrinkPairing)
	}
	fmt.Fprintf(&b, "\nIngredients:\n")
	for _, ingredient := range recipe.Ingredients {
		fmt.Fprintf(&b, "- %s", strings.TrimSpace(ingredient.Name))
		if ingredient.Quantity != "" {
			fmt.Fprintf(&b, " | quantity: %s", strings.TrimSpace(ingredient.Quantity))
		}
		if ingredient.Price != "" {
			fmt.Fprintf(&b, " | price: %s", strings.TrimSpace(ingredient.Price))
		}
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "\nInstructions:\n")
	for _, step := range recipe.Instructions {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(step))
	}
	fmt.Fprintf(&b, "\nReturn JSON only using schema_version %q.\n", recipeCritiqueSchemaV1)
	return b.String(), nil
}

func recipeCritiqueSchema() *genai.Schema {
	return &genai.Schema{
		Type:             genai.TypeObject,
		PropertyOrdering: []string{"schema_version", "overall_score", "summary", "strengths", "issues", "suggested_fixes"},
		Required:         []string{"schema_version", "overall_score", "summary", "strengths", "issues", "suggested_fixes"},
		Properties: map[string]*genai.Schema{
			"schema_version": {
				Type:        genai.TypeString,
				Description: "Return exactly recipe-critique-v1.",
			},
			"overall_score": {
				Type:        genai.TypeInteger,
				Description: "Overall quality score from 1 to 10.",
				Minimum:     float64Ptr(1),
				Maximum:     float64Ptr(10),
			},
			"summary": {
				Type:        genai.TypeString,
				Description: "A short overall verdict on the recipe.",
			},
			"strengths": {
				Type:        genai.TypeArray,
				Description: "Short strengths of the recipe.",
				Items: &genai.Schema{
					Type: genai.TypeString,
				},
			},
			"issues": {
				Type:        genai.TypeArray,
				Description: "Concrete issues that should be tracked for later tuning.",
				Items: &genai.Schema{
					Type:             genai.TypeObject,
					PropertyOrdering: []string{"severity", "category", "detail"},
					Required:         []string{"severity", "category", "detail"},
					Properties: map[string]*genai.Schema{
						"severity": {
							Type:        genai.TypeString,
							Format:      "enum",
							Enum:        []string{"low", "medium", "high"},
							Description: "Issue severity.",
						},
						"category": {
							Type:        genai.TypeString,
							Format:      "enum",
							Enum:        []string{"cookability", "safety", "clarity", "flavor", "timing", "cost", "nutrition", "ingredient_usage", "presentation"},
							Description: "Issue category.",
						},
						"detail": {
							Type:        genai.TypeString,
							Description: "Concrete explanation of the issue.",
						},
					},
				},
			},
			"suggested_fixes": {
				Type:        genai.TypeArray,
				Description: "Short suggestions to improve the recipe.",
				Items: &genai.Schema{
					Type: genai.TypeString,
				},
			},
		},
	}
}

func compactTrimmed(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func compactIssues(values []RecipeCritiqueIssue) []RecipeCritiqueIssue {
	out := make([]RecipeCritiqueIssue, 0, len(values))
	for _, value := range values {
		if value.Detail == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func float64Ptr(v float64) *float64 {
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
