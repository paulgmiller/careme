package ai

import (
	"careme/internal/kroger"
	"careme/internal/locations"
	"fmt"
	"strings"
	"time"

	"github.com/alpkeskin/gotoon"
)

// SystemMessage is shared so provider-specific clients can reuse identical behavior.
const SystemMessage = `You are a professional chef and recipe developer that wants to help working families cook each night with varied cuisines.

# Objective
Generate distinct, practical recipes using the provided constraints to maximize ingredient freshness, quality, and value while ensuring meal variety.

# Instructions
- Each meal must feature a protein and at least one side of either a vegetable and/or a starch. A combined dish (such as a pasta, stew, or similar) that incorporates a vegetable or starch alongside protein is acceptable and satisfies the side requirement.
- Recipes should use diverse cooking methods and represent a variety of cuisines.
- Provide clear, step-by-step instructions and an ingredient list for each recipe.
- Recipes should take under 1 hour to prepare, unless the user asks for something longer.
- Optionally include a wine pairing suggestion for each recipe if appropriate. Suggest a local brand if possible.
- Prioritize ingredients that are on sale (the bigger the discount, the higher the priority).

# Output Format
- Each recipe includes:
  - Title
  - Description: Try to sell the dish and add some flair.
  - Ingredient list: should include quantities and price if in input.
  - Step-by-step instructions starting with prep. Don't prefix with numbers.
  - A guess at calorie count and healthiness.
  - Optional wine or beer pairing.

# Planning & Verification
- Before generating each recipe, reference your checklist to ensure variety in cooking methods and cuisines, and confirm ingredient prioritization matches sale/seasonal data.`

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func userMessage(msg string) chatMessage {
	return chatMessage{Role: "user", Content: msg}
}

func assistantMessage(msg string) chatMessage {
	return chatMessage{Role: "assistant", Content: msg}
}

// buildRecipeMessages creates separate messages for the LLM to process more efficiently.
func (c *Client) buildRecipeMessages(location *locations.Location, saleIngredients []kroger.Ingredient, instructions string, date time.Time, lastRecipes []string) ([]chatMessage, error) {
	messages := []chatMessage{
		userMessage("Each recipe should serve 2 people."),
		userMessage("generate 3 recipes"),
		userMessage("Permitted cooking methods: oven, stove, grill, slow cooker"),
		userMessage("Prioritize ingredients that are in season for the current date and user's state location " + date.Format("January 2nd") + " in " + location.State + "."),
	}

	ingredientsMessage := "Ingredients currently on sale in TOON format\n"
	encoded, err := gotoon.Encode(saleIngredients)
	if err != nil {
		return nil, fmt.Errorf("failed to encode ingredients to TOON: %w", err)
	}
	ingredientsMessage += encoded
	messages = append(messages, userMessage(ingredientsMessage))

	if len(lastRecipes) > 0 {
		var prevRecipesMsg strings.Builder
		prevRecipesMsg.WriteString("Avoid recipes similar to these from the past 2 weeks:\n")
		for _, recipe := range lastRecipes {
			prevRecipesMsg.WriteString(fmt.Sprintf("%s\n", recipe))
		}
		messages = append(messages, userMessage(prevRecipesMsg.String()))
	}

	if instructions != "" {
		messages = append(messages, userMessage(instructions))
	}

	return messages, nil
}
