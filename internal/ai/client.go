package ai

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	openai "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
)

type Client struct {
	provider   string
	apiKey     string
	model      string
	httpClient *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Removed custom OpenAIRequest/OpenAIResponse in favor of official SDK types

func NewClient(provider, apiKey, model string) *Client {
	return &Client{
		provider:   provider,
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{},
	}
}

func (c *Client) GenerateRecipes(location string, saleIngredients []string, previousRecipes []string) (string, error) {
	prompt := c.buildRecipePrompt(location, saleIngredients, previousRecipes)

	messages := []Message{
		{
			Role:    "system",
			Content: "You are a professional chef and recipe developer that wants to help working families cook each night and introduce them to varied cuisines.",
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	switch strings.ToLower(c.provider) {
	case "openai":
		return c.generateWithOpenAI(messages)
	//case "anthropic":
	//		return c.generateWithAnthropic(messages)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", c.provider)
	}
}

func (c *Client) generateWithOpenAI(messages []Message) (string, error) {
	ctx := context.Background()
	client := openai.NewClient(option.WithAPIKey(c.apiKey))

	// Convert internal messages to SDK message params
	var chatMsgs []openai.ChatCompletionMessageParamUnion
	for _, m := range messages {
		role := strings.ToLower(m.Role)
		switch role {
		case "system":
			chatMsgs = append(chatMsgs, openai.SystemMessage(m.Content))
		case "assistant":
			chatMsgs = append(chatMsgs, openai.AssistantMessage(m.Content))
		default: // treat everything else as user
			chatMsgs = append(chatMsgs, openai.UserMessage(m.Content))
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModelGPT5,
		Messages: chatMsgs,
	}

	resp, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("openai chat completion error: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in OpenAI response")
	}

	return resp.Choices[0].Message.Content, nil
}

func (c *Client) buildRecipePrompt(location string, saleIngredients, previousRecipes []string) string {
	prompt := fmt.Sprintf("Generate 4 unique weekly recipes for location: %s\n\n", location)

	/*prompt += "Requirements:\n"
	prompt += "- Generate exactly 4 recipes\n"
	prompt += "- Prioritize ingredients currently on sale\n"
	prompt += "- Avoid repeating previous recipes\n"
	prompt += "- Include variety in cooking methods and cuisines\n"
	prompt += "- Each recipe should serve 2 people\n"
	prompt += "- Provide clear, step-by-step instructions\n\n"*/

	prompt += `Generate 3 unique, practical recipes based on the provided constraints
		Each meal should have a protein and a vegetable and/or a starch side.
		Prioritize ingredients currently on sale (bigger sale more important than small sale)
		Prioritize seasonal ingredients (currently september 3rd in washington state)
		Include variety in cooking methods and cuisines. Each recipe should serve 2 people
		Provide clear, step-by-step instructions and a total ingredient list
		Should generally take less than an hour though special ones can go over.
		Optionally provide a wine pairing with each recipe.

		Proteins and Vegatables currently avaialable (assume most starches and seasonings are available):`

	if len(saleIngredients) > 0 {
		prompt += "Ingredients currently on sale at local QFC/Fred Meyer:\n"
		for _, ingredient := range saleIngredients {
			prompt += fmt.Sprintf("- %s\n", ingredient)
		}
		prompt += "\n"
	}

	if len(previousRecipes) > 0 {
		prompt += "DO NOT repeat these recipes from the past 2 weeks:\n"
		for _, recipe := range previousRecipes {
			prompt += fmt.Sprintf("- %s\n", recipe)
		}
		prompt += "\n"
	}

	/*
			prompt += "Format your response as valid JSON with this structure:\n"
			prompt += `{
		  "recipes": [
		    {
		      "name": "Recipe Name",
		      "description": "Brief description",
		      "ingredients": ["ingredient 1", "ingredient 2"],
		      "instructions": ["step 1", "step 2"]
		    }
		  ]
	*/
	return prompt
}
