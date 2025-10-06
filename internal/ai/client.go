package ai

import (
	"careme/internal/locations"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

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

func (c *Client) GenerateRecipes(location *locations.Location, saleIngredients []string, instructions string, date time.Time, lastRecipes []string) (string, error) {
	prompt := c.buildRecipePrompt(location, saleIngredients, instructions, date, lastRecipes)

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

func (c *Client) buildRecipePrompt(location *locations.Location, saleIngredients []string, instructions string, date time.Time, lastRecipes []string) string {

	//TODO pull out meal count and people.
	//TODO json formatting
	//TODO store prompt in cache?
	prompt := `# Objective
Generate 3 distinct, practical recipes using the provided constraints to maximize ingredient efficiency and meal variety while maintaining clear, user-friendly HTML output.

# Instructions
- Each meal must feature a protein and at least one side of either a vegetable and/or a starch. A combined dish (such as a pasta, stew, or similar) that incorporates a vegetable or starch alongside protein is acceptable and satisfies the side requirement.
- Prioritize ingredients that are on sale (the bigger the discount, the higher the priority) and that are in season for the current date and user's state location ` + date.Format("January 2nd") + `  in ` + location.State + `).
- Recipes should use diverse cooking methods and represent a variety of cuisines.
- Each recipe should serve 2 people.
- Provide clear, step-by-step instructions and an ingredient list for each recipe.
- Recipes should take under 1 hour to prepare, unless a special dish requires longer.
- Optionally include a wine pairing suggestion for each recipe if appropriate.
- Permitted cooking methods: oven, stove, grill, slow cooker.


# Output Format
- Return all output as HTML suitable for inlining in a ' < div > ' element.
- Main wrapper is a ' < div > '.
- Each recipe must be within a ' < section > ' or ' < article > ', starting with a ' < h2 > ' as the recipe title.
- Each recipe includes:
  - Short description (' < p > ').
  - Ingredient list (' < ul > ' or ' < ol > ', each ingredient in a ' < li > '). Showing sale prices if applicable.
  - Step-by-step instructions (' < ol > ').
	- A guess at calorie count and helthyness in a ' < p > '.
  - Optional wine pairing (' < p > ' at end).
- Place an overall combined ingredient summary as a ' < ul > ' at the bottom. Use a ' < table > ' for extra clarity if needed. Include name, quantity and sale prices.


# Verbosity
- Be concise but clear in step-by-step instructions and ingredient lists.

# Stop Conditions
- Only return output when 3 usable recipes or a user-friendly error message is generated.

# Planning & Verification
- Before generating each recipe, reference your checklist to ensure variety in cooking methods and cuisines, and confirm ingredient prioritization matches sale/seasonal data.
- After HTML generation, validate that the HTML structure is semantically correct and ready for inlining; self-correct if necessary before handing output back.

# Inputs`

	if len(saleIngredients) > 0 {
		prompt += "Ingredients currently on sale at local QFC/Fred Meyer:\n"
		for _, ingredient := range saleIngredients {
			prompt += fmt.Sprintf("- %s\n", ingredient)
		}
		prompt += "\n"
	}

	if prevRecipes := lastRecipes; len(prevRecipes) > 0 {
		prompt += "# Previous Recipes \n"
		prompt += "DO NOT repeat these recipes from the past 2 weeks:\n"
		for _, recipe := range prevRecipes {
			prompt += fmt.Sprintf("- %s\n", recipe)
		}
		prompt += "\n"
	}

	prompt += `# Additional User Instructions:\n` + instructions + "\n"
	/*
		if len(previousRecipes) > 0 {
			prompt += "DO NOT repeat these recipes from the past 2 weeks:\n"
			for _, recipe := range previousRecipes {
				prompt += fmt.Sprintf("- %s\n", recipe)
			}
			prompt += "\n"
		}


	*/
	return prompt
}
