package ai

import (
	"bytes"
	"careme/internal/kroger"
	"careme/internal/locations"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/invopop/jsonschema"
)

const (
	defaultOpenRouterModel    = "openai/gpt-5.2"
	defaultOpenRouterEndpoint = "https://openrouter.ai/api/v1/chat/completions"
)

type Client struct {
	apiKey string
	schema map[string]any
	model  string

	endpoint     string
	httpClient   *http.Client
	conversation *conversationStore
}

type conversationStore struct {
	mu    sync.RWMutex
	items map[string][]chatMessage
}

func newConversationStore() *conversationStore {
	return &conversationStore{items: make(map[string][]chatMessage)}
}

func (s *conversationStore) get(id string) ([]chatMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	messages, ok := s.items[id]
	if !ok {
		return nil, false
	}
	out := make([]chatMessage, len(messages))
	copy(out, messages)
	return out, true
}

func (s *conversationStore) put(id string, messages []chatMessage) {
	copyMessages := make([]chatMessage, len(messages))
	copy(copyMessages, messages)
	s.mu.Lock()
	s.items[id] = copyMessages
	s.mu.Unlock()
}

// NewClient creates an OpenRouter-backed AI client.
func NewClient(apiKey, model string) *Client {
	r := jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}
	schema := r.Reflect(&ShoppingList{})
	schemaJSON, _ := json.Marshal(schema)

	var m map[string]any
	_ = json.Unmarshal(schemaJSON, &m)

	selectedModel := strings.TrimSpace(model)
	if selectedModel == "" || selectedModel == "TODOMODEL" {
		selectedModel = defaultOpenRouterModel
	}

	endpoint := os.Getenv("OPENROUTER_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultOpenRouterEndpoint
	}

	return &Client{
		apiKey:       apiKey,
		schema:       m,
		model:        selectedModel,
		endpoint:     endpoint,
		httpClient:   &http.Client{Timeout: 90 * time.Second},
		conversation: newConversationStore(),
	}
}

type openRouterRequest struct {
	Model          string                   `json:"model"`
	Messages       []chatMessage            `json:"messages"`
	ResponseFormat openRouterResponseFormat `json:"response_format"`
}

type openRouterResponseFormat struct {
	Type       string               `json:"type"`
	JSONSchema openRouterJSONSchema `json:"json_schema"`
}

type openRouterJSONSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type openRouterResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage"`
}

type openRouterErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) Regenerate(ctx context.Context, newInstruction string, conversationID string) (*ShoppingList, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("conversation ID is required for regeneration")
	}
	history, ok := c.conversation.get(conversationID)
	if !ok {
		history = []chatMessage{{Role: "system", Content: SystemMessage}}
	}

	messages := append(history, userMessage(newInstruction))
	shoppingList, assistantOutput, err := c.completeStructured(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to regenerate recipes: %w", err)
	}

	messages = append(messages, assistantMessage(assistantOutput))
	c.conversation.put(conversationID, messages)

	shoppingList.ConversationID = conversationID
	return shoppingList, nil
}

// GenerateRecipes generates recipes from user constraints and sale ingredients.
func (c *Client) GenerateRecipes(ctx context.Context, location *locations.Location, saleIngredients []kroger.Ingredient, instructions string, date time.Time, lastRecipes []string) (*ShoppingList, error) {
	messages, err := c.buildRecipeMessages(location, saleIngredients, instructions, date, lastRecipes)
	if err != nil {
		return nil, fmt.Errorf("failed to build recipe messages: %w", err)
	}
	messages = append([]chatMessage{{Role: "system", Content: SystemMessage}}, messages...)

	shoppingList, assistantOutput, err := c.completeStructured(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate recipes: %w", err)
	}

	conversationID := uuid.NewString()
	messages = append(messages, assistantMessage(assistantOutput))
	c.conversation.put(conversationID, messages)

	shoppingList.ConversationID = conversationID
	return shoppingList, nil
}

func (c *Client) completeStructured(ctx context.Context, messages []chatMessage) (*ShoppingList, string, error) {
	requestBody := openRouterRequest{
		Model:    c.model,
		Messages: messages,
		ResponseFormat: openRouterResponseFormat{
			Type: "json_schema",
			JSONSchema: openRouterJSONSchema{
				Name:   "recipes",
				Strict: true,
				Schema: c.schema,
			},
		},
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal openrouter request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("failed to build openrouter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	if referer := strings.TrimSpace(os.Getenv("OPENROUTER_HTTP_REFERER")); referer != "" {
		req.Header.Set("HTTP-Referer", referer)
	}
	if title := strings.TrimSpace(os.Getenv("OPENROUTER_APP_TITLE")); title != "" {
		req.Header.Set("X-Title", title)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("openrouter request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed reading openrouter response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr openRouterErrorResponse
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
			return nil, "", fmt.Errorf("openrouter error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return nil, "", fmt.Errorf("openrouter error (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed openRouterResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, "", fmt.Errorf("failed to decode openrouter response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return nil, "", fmt.Errorf("openrouter returned no choices")
	}

	content, err := messageContent(parsed.Choices[0].Message.Content)
	if err != nil {
		return nil, "", err
	}

	if len(parsed.Usage) > 0 {
		slog.InfoContext(ctx, "API usage", slog.Any("usage", json.RawMessage(parsed.Usage)))
	}

	content = stripCodeFence(content)
	var shoppingList ShoppingList
	if err := json.Unmarshal([]byte(content), &shoppingList); err != nil {
		return nil, "", fmt.Errorf("failed to parse AI response: %w", err)
	}

	return &shoppingList, content, nil
}

func messageContent(raw json.RawMessage) (string, error) {
	var content string
	if err := json.Unmarshal(raw, &content); err == nil {
		if strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("openrouter returned empty response content")
		}
		return content, nil
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", fmt.Errorf("unable to parse response message content: %w", err)
	}

	var builder strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			builder.WriteString(part.Text)
		}
	}
	content = strings.TrimSpace(builder.String())
	if content == "" {
		return "", fmt.Errorf("openrouter returned empty text content")
	}
	return content, nil
}

func stripCodeFence(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}
