package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

type OpenAIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
}

type OpenAIResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

type AnthropicRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
}

type AnthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

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
			Content: "You are a professional chef and recipe developer. Generate exactly 4 unique, practical recipes based on the provided constraints. Format your response as JSON with an array of recipe objects, each containing: name, description, ingredients (array), and instructions (array).",
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	switch strings.ToLower(c.provider) {
	case "openai":
		return c.generateWithOpenAI(messages)
	case "anthropic":
		return c.generateWithAnthropic(messages)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", c.provider)
	}
}

func (c *Client) generateWithOpenAI(messages []Message) (string, error) {
	request := OpenAIRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   2000,
		Temperature: 0.7,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal OpenAI request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create OpenAI request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make OpenAI request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenAI API request failed with status: %d", resp.StatusCode)
	}

	var openAIResp OpenAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return "", fmt.Errorf("failed to decode OpenAI response: %w", err)
	}

	if len(openAIResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in OpenAI response")
	}

	return openAIResp.Choices[0].Message.Content, nil
}

func (c *Client) generateWithAnthropic(messages []Message) (string, error) {
	request := AnthropicRequest{
		Model:       c.model,
		Messages:    messages,
		MaxTokens:   2000,
		Temperature: 0.7,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Anthropic request: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create Anthropic request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make Anthropic request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Anthropic API request failed with status: %d", resp.StatusCode)
	}

	var anthropicResp AnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		return "", fmt.Errorf("failed to decode Anthropic response: %w", err)
	}

	if len(anthropicResp.Content) == 0 {
		return "", fmt.Errorf("no content in Anthropic response")
	}

	return anthropicResp.Content[0].Text, nil
}

func (c *Client) buildRecipePrompt(location string, saleIngredients, previousRecipes []string) string {
	prompt := fmt.Sprintf("Generate 4 unique weekly recipes for location: %s\n\n", location)

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

	prompt += "Requirements:\n"
	prompt += "- Generate exactly 4 recipes\n"
	prompt += "- Prioritize ingredients currently on sale\n"
	prompt += "- Avoid repeating previous recipes\n"
	prompt += "- Include variety in cooking methods and cuisines\n"
	prompt += "- Each recipe should serve 2 people\n"
	prompt += "- Provide clear, step-by-step instructions\n\n"
	/*
		You are a professional chef and recipe developer. Generate 3 unique, practical recipes based on the provided constraints
		Each meal should have a protein and a vegtable and/or a starch side.
		Prioritize ingredients currently on sale Prioritize seasonal ingredients (currently september 1st in washington state)
		Include variety in cooking methods and cuisines. Each recipe should serve 2 people
		Provide clear, step-by-step instructions and a total ingredient list
		Should generally take less than an hour though special ones can go over.
		Optionally provide a wine pairing with each recipe.

		Proteins and Vegatables currently avaialable (assume most starches and seasonings are available):
	*/

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
}`

	return prompt
}
