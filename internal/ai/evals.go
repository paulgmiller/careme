package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/invopop/jsonschema"
	openai "github.com/openai/openai-go/v3"
)

// EvalMessage is the simplified message shape used by the Evals API.
type EvalMessage struct {
	Content string `json:"content"`
	Role    string `json:"role"`
	Type    string `json:"type,omitempty"`
}

func recipeJSONSchema() map[string]any {
	r := jsonschema.Reflector{
		DoNotReference: true,
		ExpandedStruct: true,
	}
	recipesSchema := r.Reflect(&ShoppingList{})
	recipesSchemaJSON, _ := json.Marshal(recipesSchema)

	var schema map[string]any
	_ = json.Unmarshal(recipesSchemaJSON, &schema)
	return schema
}

// RecipeEvalSchema returns the same structured output schema used in production.
func RecipeEvalSchema() map[string]any {
	return recipeJSONSchema()
}

// RecipeEvalModel returns the model used for recipe generation.
func RecipeEvalModel() string {
	return string(openai.ChatModelGPT5_4)
}

// RecipeEvalMessages mirrors the production recipe prompt construction for evals.
func RecipeEvalMessages(locationState string, ingredientsTSV string, ingredientCount int, instructions []string, date time.Time, lastRecipes []string) []EvalMessage {
	var messages []EvalMessage

	messages = append(messages, EvalMessage{
		Content: strings.TrimSpace(systemMessage),
		Role:    "system",
		Type:    "message",
	})

	appendUser := func(content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		messages = append(messages, EvalMessage{
			Content: content,
			Role:    "user",
			Type:    "message",
		})
	}

	if locationState == "" {
		locationState = "unknown"
	}
	appendUser(fmt.Sprintf("Prioritize ingredients that are in season for the current date and user's state location %s in %s.", date.Format("January 2nd"), locationState))
	appendUser("Default: each recipe should serve 2 people.")
	appendUser("Default: generate 3 recipes")
	appendUser("Default: prep and cook time under 1 hour")
	appendUser("Default: cooking methods: oven, stove, grill, slow cooker")
	appendUser(fmt.Sprintf("%d ingredients available in TSV format with header.\n%s", ingredientCount, strings.TrimSpace(ingredientsTSV)))

	if len(lastRecipes) > 0 {
		var prevRecipes strings.Builder
		prevRecipes.WriteString("Avoid recipes similar to these previously cooked:\n")
		for _, recipe := range lastRecipes {
			recipe = strings.TrimSpace(recipe)
			if recipe == "" {
				continue
			}
			prevRecipes.WriteString(recipe)
			prevRecipes.WriteByte('\n')
		}
		appendUser(prevRecipes.String())
	}

	for _, instruction := range instructions {
		appendUser(instruction)
	}

	return messages
}
