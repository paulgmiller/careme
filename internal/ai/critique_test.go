package ai

import (
	"strings"
	"testing"
)

func TestBuildRecipeCritiquePrompt(t *testing.T) {
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
		OriginHash:   "internal-metadata",
		Saved:        true,
	}

	prompt, err := buildRecipeCritiquePrompt(recipe)
	if err != nil {
		t.Fatalf("buildRecipeCritiquePrompt returned error: %v", err)
	}
	for _, want := range []string{
		`"title": "Roast Chicken"`,
		`"cook_time": "45 minutes"`,
		`"name": "Chicken"`,
		`"quantity": "1 whole"`,
		`"price": "$12"`,
		`"instructions": [`,
		`"Roast until golden."`,
		`Recipe JSON:`,
		`Return JSON only using schema_version "recipe-critique-v1".`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{
		`"origin_hash"`,
		`"previously_saved"`,
	} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("did not expect prompt to contain %q, got:\n%s", unwanted, prompt)
		}
	}
}

func TestParseRecipeCritique(t *testing.T) {
	critique, err := parseRecipeCritique(`{
		"schema_version": "recipe-critique-v1",
		"overall_score": 8,
		"summary": "Strong draft.",
		"strengths": ["balanced flavors"],
		"issues": [{"severity": "HIGH", "category": "Timing", "detail": "Reduce the sauce longer."}],
		"suggested_fixes": [" simmer longer "]
	}`)
	if err != nil {
		t.Fatalf("parseRecipeCritique returned error: %v", err)
	}
	if critique.Summary != "Strong draft." {
		t.Fatalf("unexpected summary: %#v", critique)
	}
	if len(critique.Strengths) != 1 || critique.Strengths[0] != "balanced flavors" {
		t.Fatalf("unexpected strengths: %#v", critique.Strengths)
	}
	if len(critique.Issues) != 1 || critique.Issues[0].Severity != "high" || critique.Issues[0].Category != "timing" || critique.Issues[0].Detail != "Reduce the sauce longer." {
		t.Fatalf("unexpected issues: %#v", critique.Issues)
	}
	if len(critique.SuggestedFixes) != 1 || critique.SuggestedFixes[0] != "simmer longer" {
		t.Fatalf("unexpected suggested fixes: %#v", critique.SuggestedFixes)
	}
}

func TestParseRecipeCritiqueRequiresScoreRange(t *testing.T) {
	_, err := parseRecipeCritique(`{"schema_version":"recipe-critique-v1","overall_score":11,"summary":"too high","strengths":[],"issues":[],"suggested_fixes":[]}`)
	if err == nil || !strings.Contains(err.Error(), "overall score") {
		t.Fatalf("expected score validation error, got %v", err)
	}
}

func TestRecipeCritiqueJSONSchemaTracksStruct(t *testing.T) {
	schema := recipeCritiqueJSONSchema()

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected top-level properties object, got %#v", schema["properties"])
	}
	if _, ok := properties["schema_version"]; !ok {
		t.Fatal("expected schema_version in reflected schema")
	}
	if _, ok := properties["overall_score"]; !ok {
		t.Fatal("expected overall_score in reflected schema")
	}
	if _, ok := properties["model"]; ok {
		t.Fatal("did not expect internal metadata field model in reflected schema")
	}
	if _, ok := properties["critiqued_at"]; ok {
		t.Fatal("did not expect internal metadata field critiqued_at in reflected schema")
	}

	overallScore, ok := properties["overall_score"].(map[string]any)
	if !ok {
		t.Fatalf("expected overall_score schema object, got %#v", properties["overall_score"])
	}
	if got := overallScore["minimum"]; got != float64(1) {
		t.Fatalf("expected reflected minimum 1, got %#v", got)
	}
	if got := overallScore["maximum"]; got != float64(10) {
		t.Fatalf("expected reflected maximum 10, got %#v", got)
	}
}
