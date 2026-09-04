package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/invopop/jsonschema"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

type Cocktail struct {
	Title        string       `json:"title"`
	Description  string       `json:"description"`
	SeasonalNote string       `json:"seasonal_note"`
	Glass        string       `json:"glass"`
	Ingredients  []Ingredient `json:"ingredients" jsonschema:"minItems=2"`
	Instructions []string     `json:"instructions" jsonschema:"minItems=1"`
}

type CocktailMenu struct {
	Cocktails []Cocktail `json:"cocktails" jsonschema:"minItems=3,maxItems=3"`
}

const cocktailInstructions = `You are a thoughtful home bartender. Suggest exactly three distinct, balanced alcoholic cocktails, each yielding one serving, using the provided Kroger catalog. Choose fresh fruit and herbs that suit the supplied date and US store location; explain the seasonal connection without claiming local provenance. Only use catalog ingredients, except ice, water and granulated sugar, whose id must be empty. Use exact catalog product IDs for every other ingredient, including spirits and garnishes. Never assume a spirit is stocked when absent from the catalog. Ignore irrelevant search matches such as meat, candy, scented products or prepared meals. Favor a small, reusable set of bottles and different styles of drink. Include precise quantities in ounces, teaspoons or counts, glassware, ice, muddling/shaking/stirring, straining and garnish instructions. Explain how to make any syrup from listed ingredients, including its ratio and cooling time. Use a modest single serving of spirits. Do not include wine pairings, dinner recipes, invented prices or health claims. Ingredient names and quantities should be plain culinary language. The description is one appetizing sentence; seasonal_note is one short explanation.`

func (c *client) GenerateCocktails(ctx context.Context, contextText string, ingredients []InputIngredient) (*CocktailMenu, error) {
	catalog, err := json.Marshal(ingredients)
	if err != nil {
		return nil, err
	}
	r := jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	raw, err := json.Marshal(r.Reflect(&CocktailMenu{}))
	if err != nil {
		return nil, err
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	resp, err := c.oai.Responses.New(ctx, responses.ResponseNewParams{
		Model: c.model, Instructions: openai.String(cocktailInstructions),
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(contextText + "\nKroger catalog:\n" + string(catalog))},
		Text:  scheme(schema),
	})
	if err != nil {
		return nil, fmt.Errorf("generate cocktails: %w", err)
	}
	slog.InfoContext(ctx, "API usage", "ai_category", "cocktails", "model", c.model, responseUsageLogAttr(c.model, resp.Usage))
	var menu CocktailMenu
	if err := json.Unmarshal([]byte(resp.OutputText()), &menu); err != nil {
		return nil, fmt.Errorf("parse cocktails: %w", err)
	}
	if err := ValidateCocktails(&menu, ingredients); err != nil {
		return nil, err
	}
	return &menu, nil
}

func ValidateCocktails(menu *CocktailMenu, catalog []InputIngredient) error {
	if menu == nil || len(menu.Cocktails) != 3 {
		return fmt.Errorf("expected exactly three cocktails")
	}
	products := make(map[string]bool, len(catalog))
	for _, p := range catalog {
		products[p.ProductID] = true
	}
	titles := map[string]bool{}
	for _, drink := range menu.Cocktails {
		title := strings.ToLower(strings.TrimSpace(drink.Title))
		if title == "" || titles[title] || strings.TrimSpace(drink.Description) == "" || strings.TrimSpace(drink.SeasonalNote) == "" || strings.TrimSpace(drink.Glass) == "" || len(drink.Ingredients) < 2 || len(drink.Instructions) == 0 {
			return fmt.Errorf("incomplete or duplicate cocktail")
		}
		titles[title] = true
		for _, step := range drink.Instructions {
			if strings.TrimSpace(step) == "" {
				return fmt.Errorf("empty cocktail instruction")
			}
		}
		for _, ing := range drink.Ingredients {
			if strings.TrimSpace(ing.Name) == "" || strings.TrimSpace(ing.Quantity) == "" {
				return fmt.Errorf("incomplete cocktail ingredient")
			}
			// Let the prompt guide ingredient selection. Free-form names include
			// preparation notes (such as "ice for shaking"), so an empty ID
			// should not invalidate the whole menu. Only verify claimed links.
			if ing.ProductID != "" && !products[ing.ProductID] {
				return fmt.Errorf("unknown cocktail product %q", ing.ProductID)
			}
		}
	}
	return nil
}
