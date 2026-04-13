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
	}

	prompt, err := buildRecipeCritiquePrompt(recipe)
	if err != nil {
		t.Fatalf("buildRecipeCritiquePrompt returned error: %v", err)
	}
	for _, want := range []string{
		"Title: Roast Chicken",
		"Cook time: 45 minutes",
		"- Chicken | quantity: 1 whole | price: $12",
		"- Roast until golden.",
		`Return JSON only using schema_version "recipe-critique-v1".`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", want, prompt)
		}
	}
}

func TestParseRecipeCritique(t *testing.T) {
	critique, err := parseRecipeCritique(`{
		"schema_version": "recipe-critique-v1",
		"overall_score": 8,
		"summary": " Strong draft. ",
		"strengths": [" balanced flavors ", ""],
		"issues": [{"severity": "HIGH", "category": "Timing", "detail": " Reduce the sauce longer. "}],
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
