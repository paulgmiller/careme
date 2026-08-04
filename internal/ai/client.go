package ai

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"careme/internal/logsetup"

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

// ignoring model for now.
func NewClient(apiKey, _ string, httpClient *http.Client, promptRecorder PromptRecorder) *client {
	// ignor model for now.
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
		model:          defaultRecipeModel,
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
		responses.ResponseInputContentParamOfInputText(msg),
	}
	content[0].OfInputText.PromptCacheBreakpoint = responses.NewResponseInputTextPromptCacheBreakpointParam()
	return responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser)
}

func recipePromptCacheKey(ctx context.Context) string {
	// The user/session fallback supports old saved recipes and callers without store
	// context. Combining both avoids collisions from shared background session names.
	// https://developers.openai.com/api/docs/guides/prompt-caching#improve-cache-hit-rates-with-a-prompt-cache-key
	userID, _ := logsetup.UserIDFromContext(ctx)
	sessionID, _ := logsetup.SessionIDFromContext(ctx)
	identity := userID + "\x00" + sessionID
	if identity == "\x00" {
		identity = "anonymous"
	}
	sum := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("careme:recipe:v1:%x", sum[:12])
}

func storeDayPromptCacheKey(storeID, date string) string {
	identity := strings.TrimSpace(storeID) + "\x00" + date
	sum := sha256.Sum256([]byte(identity))
	return fmt.Sprintf("careme:store-day:v1:%x", sum[:12])
}

func responsePromptCacheKey(ctx context.Context, ref ResponseRef) string {
	if cacheKey := strings.TrimSpace(ref.PromptCacheKey); cacheKey != "" {
		return cacheKey
	}
	return recipePromptCacheKey(ctx)
}

func configureRecipePromptCache(params *responses.ResponseNewParams, cacheKey string) {
	// These controls are specific to GPT-5.6 and later OpenAI models. Stable prompt
	// ordering is portable, but OpenRouter and other providers use different cache
	// controls (for example session_id and model-dependent cache_control blocks).
	// Introduce a provider/model-specific cache policy before routing recipe requests
	// anywhere other than the direct OpenAI GPT-5.6 client.
	params.PromptCacheKey = openai.String(cacheKey)
	params.PromptCacheOptions = responses.ResponseNewParamsPromptCacheOptions{
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
