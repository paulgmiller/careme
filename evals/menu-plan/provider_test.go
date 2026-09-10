package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMenuPlanner struct {
	location     *locations.Location
	ingredients  []ai.InputIngredient
	instructions []string
	date         time.Time
	lastRecipes  []string
	count        int
	plan         *ai.MenuPlan
	err          error
}

func (s *stubMenuPlanner) CreateMenuPlan(_ context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error) {
	s.location = location
	s.ingredients = ingredients
	s.instructions = instructions
	s.date = date
	s.lastRecipes = lastRecipes
	s.count = count
	return s.plan, s.err
}

func TestRunEvalReturnsMenuPlanJSON(t *testing.T) {
	planner := &stubMenuPlanner{plan: &ai.MenuPlan{
		Plans: []ai.RecipePlan{{
			Cuisine:          "Italian",
			AnchorIngredient: "Chicken Thighs",
			DishFormat:       "sheet-pan/roast",
			SideVegetable:    "Broccoli",
		}},
		ChefNoteSuggestion: "faster dinners",
		ResponseID:         "resp-menu",
		PromptCacheKey:     "cache-key",
	}}
	ctx := []byte(`{
		"vars": {
			"location": {"id": "store-1", "state": "WA"},
			"ingredients": [{"id": "chicken", "description": "Chicken Thighs"}],
			"instructions": "Keep dinner quick",
			"date": "2026-08-21",
			"last_recipes": ["Chicken soup"]
		}
	}`)

	result, err := runEval(ctx, planner, &stubMenuJudge{})
	require.NoError(t, err)

	output, ok := result["output"].(string)
	require.True(t, ok)
	assert.IsType(t, int64(0), result["latencyMs"])
	assert.JSONEq(t, `{
		"plans":[{"cuisine":"Italian","anchor_ingredient":"Chicken Thighs","dish_format":"sheet-pan/roast","side_vegetable":"Broccoli","fancy":false,"recipe_instructions":null}],
		"chef_note_suggestion":"faster dinners"
	}`, output)
	assert.Equal(t, "store-1", planner.location.ID)
	assert.Equal(t, []string{"Keep dinner quick"}, planner.instructions)
	assert.Equal(t, time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC), planner.date)
	assert.Equal(t, []string{"Chicken soup"}, planner.lastRecipes)
	assert.Equal(t, 1, planner.count)
	assert.NotContains(t, output, "resp-menu")
	assert.NotContains(t, output, "cache-key")
}

func TestRunEvalRejectsInvalidDate(t *testing.T) {
	ctx := []byte(`{
		"vars": {
			"location": {"id": "store-1"},
			"ingredients": [{"id": "chicken"}],
			"date": "August 21"
		}
	}`)

	result, err := runEval(ctx, &stubMenuPlanner{}, &stubMenuJudge{})

	assert.Nil(t, result)
	require.ErrorContains(t, err, `invalid eval date "August 21"`)
}

func TestRunEvalReturnsPlannerError(t *testing.T) {
	planner := &stubMenuPlanner{err: errors.New("model unavailable")}
	ctx := []byte(`{
		"vars": {
			"location": {"id": "store-1"},
			"ingredients": [{"id": "chicken"}],
			"date": "2026-08-21"
		}
	}`)

	result, err := runEval(ctx, planner, &stubMenuJudge{})

	assert.Nil(t, result)
	require.EqualError(t, err, "failed to create menu plan: model unavailable")
}

func TestRunEvalRejectsInvalidJSON(t *testing.T) {
	result, err := runEval([]byte(`{"vars":`), &stubMenuPlanner{}, &stubMenuJudge{})

	assert.Nil(t, result)
	require.ErrorContains(t, err, "failed to decode Promptfoo context")
}

type stubMenuJudge struct {
	request evalCase
	plan    ai.MenuPlan
	err     error
}

func (s *stubMenuJudge) Judge(_ context.Context, request evalCase, plan ai.MenuPlan) (*menuCritique, error) {
	s.request, s.plan = request, plan
	return &menuCritique{RequestFidelity: 9, CulinaryCoherence: 9, MeaningfulVariety: 9, Summary: "Good menu", Issues: []menuCritiqueIssue{}}, s.err
}

const validMenuCritique = `{"request_fidelity":9,"culinary_coherence":8,"meaningful_variety":10,"summary":"The menu meets the request.","issues":[]}`

func TestParseMenuCritique(t *testing.T) {
	for _, tt := range []struct{ name, body, wantError string }{
		{"valid", validMenuCritique, ""},
		{"empty", "", "decode menu critique"},
		{"malformed", "{", "decode menu critique"},
		{"missing score", strings.Replace(validMenuCritique, `"request_fidelity":9,`, "", 1), "score must be between"},
		{"out of range", strings.Replace(validMenuCritique, `"culinary_coherence":8`, `"culinary_coherence":11`, 1), "score must be between"},
		{"empty summary", strings.Replace(validMenuCritique, "The menu meets the request.", " ", 1), "requires summary"},
		{"missing issues", strings.Replace(validMenuCritique, `,"issues":[]`, "", 1), "issues array"},
		{"invalid reference", strings.Replace(validMenuCritique, `"issues":[]`, `"issues":[{"category":"request_fidelity","severity":"high","recipe_indexes":[4],"detail":"Missing servings","suggested_fix":"Add six servings"}]`, 1), "out of range"},
		{"invalid category", strings.Replace(validMenuCritique, `"issues":[]`, `"issues":[{"category":"other","severity":"high","recipe_indexes":[1],"detail":"Missing servings","suggested_fix":"Add six servings"}]`, 1), "invalid menu critique category"},
		{"invalid severity", strings.Replace(validMenuCritique, `"issues":[]`, `"issues":[{"category":"request_fidelity","severity":"critical","recipe_indexes":[1],"detail":"Missing servings","suggested_fix":"Add six servings"}]`, 1), "invalid menu critique severity"},
		{"missing evidence", strings.Replace(validMenuCritique, `"issues":[]`, `"issues":[{"category":"request_fidelity","severity":"high","recipe_indexes":[1]}]`, 1), "requires recipe indexes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseMenuCritique(tt.body, 3)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 9, result.RequestFidelity)
		})
	}
}

type judgeTransport func(*http.Request) (*http.Response, error)

func (f judgeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMenuJudgeAPI(t *testing.T) {
	for _, tt := range []struct {
		name, response string
		status         int
		wantError      string
	}{
		{"success", validMenuCritique, 200, ""},
		{"empty content", "", 200, "decode menu critique"},
		{"no choices", "NO_CHOICES", 200, "no choices"},
		{"API failure", "", 400, "OpenRouter menu judge"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: judgeTransport(func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, "/api/v1/chat/completions", r.URL.Path)
				var body struct {
					Model          string                           `json:"model"`
					Messages       []struct{ Role, Content string } `json:"messages"`
					ResponseFormat struct {
						Type       string
						JSONSchema struct {
							Strict bool
							Schema map[string]interface{}
						} `json:"json_schema"`
					} `json:"response_format"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "judge-model", body.Model)
				require.Len(t, body.Messages, 2)
				assert.Equal(t, "system", body.Messages[0].Role)
				assert.Equal(t, "user", body.Messages[1].Role)
				var payload struct {
					Request evalCase
					Menu    ai.MenuPlan
				}
				require.NoError(t, json.Unmarshal([]byte(body.Messages[1].Content), &payload))
				assert.Equal(t, "Use saffron once", payload.Request.Instructions)
				assert.Equal(t, []string{"Old dinner"}, payload.Request.LastRecipes)
				require.Len(t, payload.Request.Ingredients, 1)
				assert.Equal(t, "WA", payload.Request.Location.State)
				assert.Equal(t, "2026-09-10", payload.Request.Date)
				require.Len(t, payload.Menu.Plans, 2)
				assert.Empty(t, payload.Menu.ResponseID)
				assert.Empty(t, payload.Menu.PromptCacheKey)
				assert.Equal(t, "json_schema", body.ResponseFormat.Type)
				assert.True(t, body.ResponseFormat.JSONSchema.Strict)
				assert.Contains(t, body.ResponseFormat.JSONSchema.Schema["properties"], "request_fidelity")
				content, err := json.Marshal(map[string]interface{}{"model": "returned-judge", "choices": []interface{}{map[string]interface{}{"message": map[string]string{"role": "assistant", "content": tt.response}}}})
				require.NoError(t, err)
				if tt.response == "NO_CHOICES" {
					content = []byte(`{"choices":[]}`)
				}
				return &http.Response{StatusCode: tt.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(content)))}, nil
			})}
			judge := newMenuJudge("fake-key", "judge-model", client)
			result, err := judge.Judge(t.Context(), evalCase{Instructions: "Use saffron once", Date: "2026-09-10", Location: locations.Location{State: "WA"}, Ingredients: []ai.InputIngredient{{Description: "Tofu"}}, LastRecipes: []string{"Old dinner"}}, ai.MenuPlan{Plans: []ai.RecipePlan{{AnchorIngredient: "Tofu"}, {AnchorIngredient: "Chicken"}}, ResponseID: "private-response", PromptCacheKey: "private-cache"})
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "returned-judge", result.Model)
			assert.False(t, result.JudgedAt.IsZero())
		})
	}
}

func TestRunEvalJudgeHandoffAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "judge failure"}[fail], func(t *testing.T) {
			judge := &stubMenuJudge{}
			if fail {
				judge.err = errors.New("judge unavailable")
			}
			planner := &stubMenuPlanner{plan: &ai.MenuPlan{Plans: []ai.RecipePlan{{RecipeInstructions: []string{"Use saffron"}}}, ResponseID: "private", PromptCacheKey: "private"}}
			result, err := runEval([]byte(`{"vars":{"location":{"id":"store"},"ingredients":[{"description":"Tofu"}],"date":"2026-09-10","instructions":"Use saffron"}}`), planner, judge)
			assert.Equal(t, "Use saffron", judge.request.Instructions)
			assert.Empty(t, judge.plan.ResponseID)
			assert.Empty(t, judge.plan.PromptCacheKey)
			assert.Equal(t, []string{"Use saffron"}, judge.plan.Plans[0].RecipeInstructions)
			if fail {
				require.ErrorContains(t, err, "judge generated menu: judge unavailable")
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			metadata := result["metadata"].(map[string]interface{})
			assert.Equal(t, 9, metadata["critique"].(*menuCritique).RequestFidelity)
			assert.IsType(t, int64(0), metadata["judgeLatencyMs"])
		})
	}
}
