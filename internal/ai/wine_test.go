package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"careme/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeWineStyle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain style", in: "Pinot Noir", want: "Pinot Noir"},
		{name: "parenthetical region hint", in: "Sauvignon Blanc (New Zealand or Loire)", want: "Sauvignon Blanc"},
		{name: "trailing punctuation", in: "  Riesling.  ", want: "Riesling"},
		{name: "bracket hint", in: "Chardonnay [California]", want: "Chardonnay"},
		{name: "empty", in: "   ", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeWineStyle(tc.in); got != tc.want {
				t.Fatalf("normalizeWineStyle(%q): got %q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeRecipeWineStyles(t *testing.T) {
	got := normalizeRecipeWineStyles([]string{
		" Pinot Noir (WA or Oregon) ",
		"pinot noir",
		"Sauvignon Blanc (New Zealand or Loire)",
		"Riesling",
	})
	want := []string{"Pinot Noir", "Sauvignon Blanc"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected normalized wine styles: got %#v want %#v", got, want)
	}
}

func TestBuildWineSelectionPrompt(t *testing.T) {
	recipe := Recipe{
		Title:        "Roast Chicken",
		Description:  "Crisp skin and herbs.",
		CookTime:     "45 minutes",
		CostEstimate: "$18-24",
		Ingredients: []Ingredient{
			{Name: "Chicken", Quantity: "1 whole", Price: "$12"},
			{Name: "Lemon", Quantity: "1", Price: "$1"},
		},
		Instructions: []string{"Roast until golden.", "Finish with lemon juice."},
		Health:       "Balanced dinner",
		DrinkPairing: "Pinot Noir",
		WineStyles:   []string{"Pinot Noir", "Chardonnay"},
	}
	wines := []InputIngredient{
		{ProductID: "pinot-noir-1", Description: "Pinot Noir", Size: "750mL", PriceRegular: new(float32)},
	}
	*wines[0].PriceRegular = 13.99

	prompt, err := buildWineSelectionPrompt(recipe, wines)
	if err != nil {
		t.Fatalf("buildWineSelectionPrompt returned error: %v", err)
	}
	expect := "Chicken\nCrisp skin and herbs."
	if !strings.Contains(prompt, expect) {
		t.Fatalf("expected recipe summary in prompt: %s\n\n got \n %s", expect, prompt)
	}
	if !strings.Contains(prompt, "Existing drink pairing note: Pinot Noir") {
		t.Fatalf("expected pairing hints in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "- Roast until golden.\n- Finish with lemon juice.\n") {
		t.Fatalf("expected instructions replay in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Candidate wines TSV:\nProductId\tBrand\tDescription\tSize\tPriceRegular\tPriceSale\npinot-noir-1\t\tPinot Noir\t750mL\t13.99\t13.99\n") {
		t.Fatalf("expected candidate wines TSV in prompt: %s", prompt)
	}
}

func TestPickWineUsesLunaWithoutReasoning(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(string(body), `"model":"`+gpt6Luna+`"`) {
			t.Fatalf("expected Luna model in request: %s", body)
		}
		if !strings.Contains(string(body), `"reasoning":{"effort":"none"}`) {
			t.Fatalf("expected no-reasoning request: %s", body)
		}
		var request struct {
			Text struct {
				Format map[string]any `json:"format"`
			} `json:"text"`
		}
		require.NoError(t, json.Unmarshal(body, &request))
		assert.Equal(t, "json_schema", request.Text.Format["type"])
		assert.Equal(t, "wine_selection", request.Text.Format["name"])
		assert.Equal(t, true, request.Text.Format["strict"])
		schema := request.Text.Format["schema"].(map[string]any)
		assert.ElementsMatch(t, []string{"wines", "commentary"}, schemaRequired(t, schema))
		assert.Equal(t, false, schema["additionalProperties"])
		properties := schema["properties"].(map[string]any)
		wineItems := properties["wines"].(map[string]any)["items"].(map[string]any)
		assert.ElementsMatch(t, []string{"id", "name", "quantity"}, schemaRequired(t, wineItems))
		assert.Equal(t, false, wineItems["additionalProperties"])
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"id": "resp-wine",
				"object": "response",
				"created_at": 1778529600,
				"status": "completed",
				"model": %q,
				"output": [{
					"id": "msg-wine",
					"type": "message",
					"status": "completed",
					"role": "assistant",
					"content": [{
						"type": "output_text",
						"text": "{\"wines\":[{\"id\":\"pinot-noir-1\",\"name\":\"Pinot Noir\",\"quantity\":\"1 bottle\"}],\"commentary\":\"Bright enough for roast chicken.\"}",
						"annotations": []
					}]
				}],
				"usage": {
					"input_tokens": 1,
					"input_tokens_details": {"cached_tokens": 0},
					"output_tokens": 1,
					"output_tokens_details": {"reasoning_tokens": 0},
					"total_tokens": 2
				}
			}`, gpt6Luna))),
			Request: req,
		}, nil
	})}, nil)

	selection, err := client.PickWine(t.Context(), Recipe{
		Title:        "Roast Chicken",
		Description:  "Crisp skin.",
		Instructions: []string{"Roast until golden."},
		DrinkPairing: "Pinot Noir",
	}, []InputIngredient{{ProductID: "pinot-noir-1", Description: "Pinot Noir"}})
	if err != nil {
		t.Fatalf("PickWine returned error: %v", err)
	}
	if len(selection.Wines) != 1 || selection.Wines[0].ProductID != "pinot-noir-1" {
		t.Fatalf("unexpected wine selection: %#v", selection)
	}
}

func TestPickWineRejectsUnusableResponses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  string
		reason  string
		content string
		wantErr string
	}{
		{name: "non-JSON text", status: "completed", content: `{"type":"output_text","text":"These wines pair well."}`, wantErr: `failed to parse wine selection response "resp-wine-error"`},
		{name: "refusal", status: "completed", content: `{"type":"refusal","refusal":"Cannot fulfill this request."}`, wantErr: `wine selection response "resp-wine-error" was refused`},
		{name: "truncated JSON", status: "incomplete", reason: "max_output_tokens", content: `{"type":"output_text","text":"{\"wines\":["}`, wantErr: `has status "incomplete" (incomplete reason: "max_output_tokens")`},
		{name: "empty output", status: "completed", content: `{"type":"output_text","text":""}`, wantErr: `failed to parse wine selection response "resp-wine-error"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := fmt.Sprintf(`{"id":"resp-wine-error","status":%q,"incomplete_details":{"reason":%q},"output":[{"type":"message","role":"assistant","content":[%s]}]}`, tc.status, tc.reason, tc.content)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}, nil)
			selection, err := client.PickWine(t.Context(), Recipe{Title: "Roast Chicken"}, []InputIngredient{{ProductID: "pinot", Description: "Pinot Noir"}})
			require.ErrorContains(t, err, tc.wantErr)
			assert.Nil(t, selection)
		})
	}
}
