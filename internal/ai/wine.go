package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

const defaultWineModel = openai.ChatModelGPT6Luna

type WineSelection struct {
	Wines      []Ingredient `json:"wines"`
	Commentary string       `json:"commentary"`
}

const winePrompt = `
Act as a sommelier for the recipe provided below
Select 1 to 2 wines from the provided TSV that best match the dish
Return JSON with wines (ingredient array) and concise commentary explaining why those specific bottles work.
Only choose wines present in the TSV. For each wine set id to the exact ProductId and include name and quantity (use "1 bottle").
Be creative not always the same safe picks. Consider the specific ingredients, cooking method, and flavor profile of the dish when making your selection.
Also for fancier/more expensive dishes consider more expensive wines.
`

func (c *client) PickWine(ctx context.Context, recipe Recipe, wines []InputIngredient) (*WineSelection, error) {
	prompt, err := buildWineSelectionPrompt(recipe, wines)
	if err != nil {
		return nil, fmt.Errorf("failed to build wine selection prompt: %w", err)
	}
	text := scheme(c.wineSchema)
	text.Format.OfJSONSchema.Name = "wine_selection"
	text.Format.OfJSONSchema.Strict = openai.Bool(true)
	params := responses.ResponseNewParams{
		Model:        c.wineModel,
		Reasoning:    noReasoning(),
		Instructions: openai.String(winePrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{user(prompt)},
		},
		Text: text,
	}
	resp, err := c.oai.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to pick wine: %w", err)
	}
	slog.InfoContext(ctx, "API usage", "ai_category", aiCategoryWine, "model", c.wineModel, responseUsageLogAttr(c.wineModel, resp.Usage, string(resp.ServiceTier)))

	if resp.Status != responses.ResponseStatusCompleted {
		return nil, fmt.Errorf("wine selection response %q has status %q (incomplete reason: %q)", resp.ID, resp.Status, resp.IncompleteDetails.Reason)
	}
	for _, item := range resp.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "refusal" {
				return nil, fmt.Errorf("wine selection response %q was refused", resp.ID)
			}
		}
	}
	var selection WineSelection
	if err := json.Unmarshal([]byte(resp.OutputText()), &selection); err != nil {
		return nil, fmt.Errorf("failed to parse wine selection response %q: %w", resp.ID, err)
	}
	return &selection, nil
}

// similiar to image generation builder
func buildWineSelectionPrompt(recipe Recipe, wines []InputIngredient) (string, error) {
	var wineTSV strings.Builder
	if err := InputIngredientsToTSV(wines, &wineTSV); err != nil {
		return "", fmt.Errorf("failed to convert wines to TSV: %w", err)
	}

	var promptBuilder strings.Builder
	fmt.Fprintf(&promptBuilder, "Recipe:\n")
	fmt.Fprintf(&promptBuilder, "%s\n", recipe.Title)
	fmt.Fprintf(&promptBuilder, "%s\n", recipe.Description)
	fmt.Fprintf(&promptBuilder, "Instructions:\n")
	for _, ins := range recipe.Instructions {
		fmt.Fprintf(&promptBuilder, "- %s\n", ins)
	}
	fmt.Fprintf(&promptBuilder, "Existing drink pairing note: %s\n", recipe.DrinkPairing)
	// add cost estimate when we believ it?
	fmt.Fprintf(&promptBuilder, "\nCandidate wines TSV:\n%s", wineTSV.String())
	return promptBuilder.String(), nil
}
