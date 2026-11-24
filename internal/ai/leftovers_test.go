package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"careme/internal/config"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func leftoversResponseClient(t *testing.T, output string, check func(map[string]any)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.True(t, strings.HasSuffix(req.URL.Path, "/responses"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		if check != nil {
			check(body)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"id":"resp-leftovers","object":"response","status":"completed","model":"test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}]}`, output)))}, nil
	})}
}

func TestSuggestLeftoversIngredients(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		valid        bool
	}{
		{"valid", `{"ingredients":["eggs"," bread ","spinach"]}`, true},
		{"too few", `{"ingredients":["eggs"]}`, false},
		{"too many", `{"ingredients":["eggs","bread","spinach","oil","salt","pepper"]}`, false},
		{"duplicate", `{"ingredients":["eggs","Eggs","spinach"]}`, false},
		{"empty", `{"ingredients":["eggs"," ","spinach"]}`, false},
		{"invalid JSON", `oops`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testAIConfig(config.DefaultRecipeModel)
			cfg.MenuModel = "unused-menu-model"
			client := NewClient(cfg, leftoversResponseClient(t, tc.output, func(body map[string]any) {
				assert.Equal(t, cfg.RecipeModel, body["model"])
				assert.NotContains(t, body, "previous_response_id")
				instructions := body["instructions"].(string)
				assert.Contains(t, instructions, "3 to 5 individual ingredients")
				encoded, _ := json.Marshal(body["input"])
				assert.Contains(t, string(encoded), "cooked rice")
				assert.Contains(t, string(encoded), "vegetarian")
			}), nil)
			got, err := client.SuggestLeftoversIngredients(t.Context(), LeftoversInput{Leftovers: "cooked rice", Meal: "lunch", Directive: "vegetarian"})
			if tc.valid {
				require.NoError(t, err)
				assert.Equal(t, []string{"eggs", "bread", "spinach"}, got)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestGenerateLeftoversMealUsesOnlyConfirmedFoodWithoutMenuPlan(t *testing.T) {
	for _, tier := range []string{"default", "flex"} {
		t.Run(tier, func(t *testing.T) {
			cfg := testAIConfig(config.DefaultRecipeModel)
			cfg.ServiceTier = responses.ResponseNewParamsServiceTier(tier)
			cfg.MenuModel = "unused-menu-model"
			output := `{"questions":[],"recipes":[{"title":"Crisp rice","ingredients":[{"id":"","name":"cooked rice","quantity":"2 cups"}],"instructions":["Reheat the 2 cups of rice."]}],"explanation":""}`
			client := NewClient(cfg, leftoversResponseClient(t, output, func(body map[string]any) {
				assert.Equal(t, cfg.RecipeModel, body["model"])
				assert.Equal(t, tier, body["service_tier"])
				assert.NotContains(t, body, "previous_response_id")
				instructions := body["instructions"].(string)
				assert.Contains(t, instructions, "confirmed ingredients, water and reasonable basics only")
				assert.Contains(t, instructions, "ask when in doubt")
				assert.Contains(t, instructions, "exactly one complete recipe")
				assert.Contains(t, instructions, "explain honestly why that is not possible")
				encoded, _ := json.Marshal(body["input"])
				assert.Contains(t, string(encoded), "unavailable_ingredients")
				assert.Contains(t, string(encoded), "Rice bowl")
				assert.Contains(t, string(encoded), `\"servings\":3`)
				assert.Contains(t, instructions, "scale every ingredient and step quantity to that yield")
			}), nil)
			recipe, err := client.GenerateLeftoversMeal(t.Context(), LeftoversInput{Leftovers: "cooked rice", Meal: "breakfast", Servings: 3, Unavailable: []string{"eggs"}, RecentMeals: []string{"Rice bowl"}})
			require.NoError(t, err)
			require.Len(t, recipe.Recipes, 1)
			assert.Equal(t, "Crisp rice", recipe.Recipes[0].Title)
			assert.Equal(t, "resp-leftovers", recipe.Recipes[0].ResponseID)
		})
	}
}

func TestLeftoversOutcomeValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  LeftoversInput
		result LeftoversResult
		valid  bool
	}{
		{name: "questions", result: LeftoversResult{Questions: []string{"eggs"}}, valid: true},
		{name: "one question remaining", input: LeftoversInput{Asked: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}}, result: LeftoversResult{Questions: []string{"eggs"}}, valid: true},
		{name: "ten questions", input: LeftoversInput{Asked: make([]string, 10)}, result: LeftoversResult{Questions: []string{"eggs"}}},
		{name: "over remaining", input: LeftoversInput{Asked: make([]string, 9)}, result: LeftoversResult{Questions: []string{"eggs", "bread"}}},
		{name: "make now", input: LeftoversInput{MakeNow: true}, result: LeftoversResult{Questions: []string{"eggs"}}},
		{name: "repeat", input: LeftoversInput{Asked: []string{"Eggs"}}, result: LeftoversResult{Questions: []string{" eggs "}}},
		{name: "unavailable", input: LeftoversInput{Unavailable: []string{"eggs"}}, result: LeftoversResult{Questions: []string{"eggs"}}},
		{name: "empty question", result: LeftoversResult{Questions: []string{" "}}},
		{name: "duplicate", result: LeftoversResult{Questions: []string{"eggs", "Eggs"}}},
		{name: "no outcome"},
		{name: "mixed outcomes", result: LeftoversResult{Questions: []string{"eggs"}, Explanation: "Cannot make dinner"}},
		{name: "incomplete recipe", result: LeftoversResult{Recipes: []Recipe{{Title: "Rice"}}}},
		{name: "honest explanation", input: LeftoversInput{MakeNow: true}, result: LeftoversResult{Explanation: "These foods do not make a cohesive dinner."}, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.result.validate(tc.input)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
