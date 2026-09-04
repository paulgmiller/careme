package ai

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"github.com/invopop/jsonschema"
)

type client struct {
	recipeSchema   map[string]any
	wineSchema     map[string]any
	menuSchema     map[string]any
	model          string
	wineModel      string
	oai            openai.Client
	promptRecorder PromptRecorder
}

// NewClient uses the production recipe model when model is empty.
func NewClient(apiKey, model string, httpClient *http.Client, promptRecorder PromptRecorder) *client {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultRecipeModel
	}
	if promptRecorder == nil {
		promptRecorder = noopPromptRecorder{}
	}
	r := jsonschema.Reflector{
		DoNotReference: true, // no $defs and no $ref
		ExpandedStruct: true, // put the root type inline (not a $ref)
	}
	recipeSchema := r.Reflect(&Recipe{})
	recipeSchemaJSON, _ := json.Marshal(recipeSchema)
	wineSchema := r.Reflect(&WineSelection{})
	wineSchemaJSON, _ := json.Marshal(wineSchema)
	menuSchema := r.Reflect(&MenuPlan{})
	menuSchemaJson, _ := json.Marshal(menuSchema)
	var recipe map[string]any
	_ = json.Unmarshal(recipeSchemaJSON, &recipe)
	var wine map[string]any
	_ = json.Unmarshal(wineSchemaJSON, &wine)
	var menu map[string]any
	_ = json.Unmarshal(menuSchemaJson, &menu)

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if httpClient != nil {
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	aiClient := openai.NewClient(opts...)

	return &client{
		oai:            aiClient,
		recipeSchema:   recipe,
		wineSchema:     wine,
		menuSchema:     menu,
		model:          model,
		wineModel:      defaultWineModel,
		promptRecorder: promptRecorder,
	}
}

func scheme(schema map[string]any) responses.ResponseTextConfigParam {
	return responses.ResponseTextConfigParam{
		Format: responses.ResponseFormatTextConfigUnionParam{
			OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
				Name:   "recipes",
				Schema: schema, // https://platform.openai.com/docs/guides/structured-outputs?example=structured-data
			},
		},
	}
}

func noReasoning() responses.ReasoningParam {
	return responses.ReasoningParam{Effort: responses.ReasoningEffortNone}
}

func (c *client) Ready(ctx context.Context) error {
	// more CORRECT to do a very simple response request with allowed tokens 1 but this seems cheaper
	// https://chatgpt.com/share/6984da16-ff88-8009-8486-4e0479ac6a01
	// could only do it once to ensure startup
	_, err := c.oai.Models.List(ctx)
	return err
}

func cleanInstructionMessages(instructions []string) []PromptMessage {
	var messages []PromptMessage
	for _, i := range instructions {
		i = strings.TrimSpace(i)
		if i == "" {
			continue
		}
		messages = append(messages, userPromptMessage(i))
	}
	return messages
}

func userPromptMessage(msg string) PromptMessage {
	return PromptMessage{Role: "user", Content: msg}
}

func user(msg string) responses.ResponseInputItemUnionParam {
	return responses.ResponseInputItemParamOfMessage(msg, responses.EasyInputMessageRoleUser)
}

func userWithCacheBreakpoint(msg string) responses.ResponseInputItemUnionParam {
	content := responses.ResponseInputMessageContentListParam{
		{
			OfInputText: &responses.ResponseInputTextParam{
				Text:                  msg,
				PromptCacheBreakpoint: responses.NewResponseInputTextPromptCacheBreakpointParam(),
			},
		},
	}
	return responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser)
}

// These cache options and cache key are specific to GPT-5.6 and later OpenAI models. Stable prompt
// ordering is portable, but OpenRouter and other providers use different cache
// controls (for example session_id and model-dependent cache_control blocks).
// Introduce a provider/model-specific cache policy before routing recipe requests
// anywhere other than the direct OpenAI GPT-5.6 client.

func storeDayPromptCacheKey(storeID, date string) string {
	identity := strings.TrimSpace(storeID) + "\x00" + date
	sum := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("careme:store-day:v1:%x", sum[:12])
}

func defaultCacheOptions() responses.ResponseNewParamsPromptCacheOptions {
	return responses.ResponseNewParamsPromptCacheOptions{
		Mode: "explicit",
		Ttl:  "30m",
	}
}

func messagesToInput(messages []PromptMessage) []responses.ResponseInputItemUnionParam {
	input := make([]responses.ResponseInputItemUnionParam, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		if msg.PromptCacheBreakpoint {
			input = append(input, userWithCacheBreakpoint(msg.Content))
			continue
		}
		input = append(input, user(msg.Content))
	}
	return input
}

func (c *client) recordRecipePrompt(ctx context.Context, responseID string, params responses.ResponseNewParams, input []PromptMessage) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return
	}
	record := &PromptRecord{
		ResponseID:         responseID,
		Model:              string(params.Model),
		Instructions:       strings.TrimSpace(params.Instructions.Or("")),
		PreviousResponseID: strings.TrimSpace(params.PreviousResponseID.Or("")),
		Input:              append([]PromptMessage(nil), input...),
	}
	if err := c.promptRecorder.RecordPrompt(ctx, record); err != nil {
		slog.ErrorContext(ctx, "failed to record recipe prompt", "response_id", responseID, "error", err)
	}
}
