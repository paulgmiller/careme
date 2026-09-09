package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"careme/internal/config"
	locations "careme/internal/locations/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecipeAndMenuServiceTier(t *testing.T) {
	for _, flex := range []bool{false, true} {
		name := "interactive"
		if flex {
			name = "flex"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			if flex {
				ctx = WithFlexProcessing(ctx)
			}
			for _, operation := range []string{"recipe", "recipe retry", "menu", "menu retry", "ingredient correction"} {
				t.Run(operation, func(t *testing.T) {
					calls := 0
					c := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						calls++
						var body map[string]any
						require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
						if flex {
							assert.Equal(t, "flex", body["service_tier"])
						} else {
							assert.NotContains(t, body, "service_tier")
						}
						output := `{"title":"Soup","ingredients":[],"instructions":["Cook."]}`
						if operation == "menu" || operation == "menu retry" || operation == "ingredient correction" {
							output = `{"plans":[{"anchor_ingredient":"Carrots","side_vegetable":"Carrots"}]}`
						}
						return menuPlanHTTPResponse(req, "resp-new", output), nil
					})}, nil)
					ref := ResponseRef{ID: "resp-parent"}
					ingredients := []InputIngredient{{Description: "Carrots", ProductID: "1"}}
					var err error
					switch operation {
					case "recipe":
						_, err = c.GenerateRecipe(ctx, nil, ref)
					case "recipe retry":
						_, err = c.Regenerate(ctx, []string{"less salt"}, ref)
					case "menu":
						_, err = c.CreateMenuPlan(ctx, &locations.Location{ID: "1", State: "WA"}, ingredients, nil, time.Now(), nil, 1)
					case "menu retry":
						_, err = c.RegenerateMenuPlan(ctx, nil, ref, 1)
					case "ingredient correction":
						_, err = c.regenerateMenuPlanForIngredientMismatch(ctx, ref, ingredients, errors.New("unavailable ingredient"), 1)
					}
					require.NoError(t, err)
					assert.Equal(t, 1, calls)
				})
			}
			assert.Empty(t, RecipeServiceTier(context.Background()))
		})
	}
}

func TestResponseSpendForTier(t *testing.T) {
	standard := estimateOpenAIResponseSpend(config.DefaultRecipeModel, 1200, 900, 200, 350)
	assert.Equal(t, standard, responseSpendForTier(standard, "default"))
	assert.Equal(t, standard, responseSpendForTier(standard, ""))
	flex := responseSpendForTier(standard, "flex")
	assert.InDelta(t, standard.totalUSD()/2, flex.totalUSD(), 1e-9)
	assert.Equal(t, standard.cacheWriteUSD/2, flex.cacheWriteUSD)
	assert.Equal(t, standard.cachedInputUSD/2, flex.cachedInputUSD)
}

func TestFlexGenerationReturnsAPIErrorWithoutStandardFallback(t *testing.T) {
	calls := 0
	c := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		assert.Equal(t, "flex", body["service_tier"])
		resp := menuPlanHTTPResponse(req, "", "")
		resp.StatusCode = http.StatusBadRequest
		return resp, nil
	})}, nil)
	recipe, err := c.GenerateRecipe(WithFlexProcessing(t.Context()), nil, ResponseRef{ID: "resp-menu"})
	require.ErrorContains(t, err, "failed to generate recipe")
	assert.Nil(t, recipe)
	assert.Equal(t, 1, calls)
}
