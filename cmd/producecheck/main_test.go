package main

import (
	"reflect"
	"testing"

	"careme/internal/ingredientcoverage"
	"careme/internal/kroger"
)

func TestSelectedDatasets_CustomTerms(t *testing.T) {
	got, err := selectedDatasets("produce", " carrots,Carrots, brussel sprouts , kale ")
	if err != nil {
		t.Fatalf("selectedDatasets() error = %v", err)
	}
	want := []ingredientcoverage.Dataset{{
		Name:  "custom",
		Label: "Custom",
		Terms: []string{"carrot", "brussels sprout", "kale"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectedDatasets() = %#v, want %#v", got, want)
	}
}

func TestSelectedDatasets_DefaultDataset(t *testing.T) {
	got, err := selectedDatasets("produce", "")
	if err != nil {
		t.Fatalf("selectedDatasets() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "produce" {
		t.Fatalf("selectedDatasets() = %#v, want produce dataset", got)
	}
}

func TestIngredientCoverageParseTermsCSV(t *testing.T) {
	tests := []struct {
		name string
		csv  string
		want []string
	}{
		{
			name: "dedupes and normalizes",
			csv:  " carrots,Carrots, brussel sprouts , kale ",
			want: []string{"carrot", "brussels sprout", "kale"},
		},
		{
			name: "drops blanks",
			csv:  " ,  , apples",
			want: []string{"apple"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ingredientcoverage.ParseTermsCSV(tc.csv)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseTermsCSV() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestNormalizeTerm(t *testing.T) {
	got := ingredientcoverage.BaselineMatcher().NormalizeTerm("  Brussel   Sprouts ")
	want := "brussels sprout"
	if got != want {
		t.Fatalf("normalizeTerm() = %q, want %q", got, want)
	}
}

func TestNormalizeTerm_RemovesParentheticalAndDiacritics(t *testing.T) {
	got := ingredientcoverage.BaselineMatcher().NormalizeTerm(" Green Onions (Scallions), Jalapeño Peppers ")
	want := "green onion jalapeno pepper"
	if got != want {
		t.Fatalf("normalizeTerm() = %q, want %q", got, want)
	}
}

func TestHasProduce_UsesTokenMatching(t *testing.T) {
	descriptions := []*string{
		strPtr("Fresh Seedless Mini Cucumbers"),
		strPtr("Fresh Jalapeno Peppers"),
		strPtr("Simple Truth Organic® Whole Baby Bella Mushrooms"),
		strPtr("Simple Truth Organic® Kiwifruit"),
	}

	report := ingredientcoverage.Analyze(ingredientcoverage.Dataset{
		Name:  "custom",
		Label: "Custom",
		Terms: []string{"seedless cucumbers", "jalapeño peppers", "baby bella (cremini) mushrooms", "kiwi"},
	}, wrapIngredients(descriptions), ingredientcoverage.BaselineMatcher())
	if report.MatchedTerms != 4 {
		t.Fatalf("MatchedTerms = %d, want 4", report.MatchedTerms)
	}
}

func TestSummarizeFilterMatches(t *testing.T) {
	descriptions := []*string{
		strPtr("Fresh Seedless Mini Cucumbers"),
		strPtr("Fresh Mini Cucumbers"),
		strPtr("Fresh Jalapeno Peppers"),
		strPtr("Simple Truth Organic® Kiwifruit"),
	}

	report := ingredientcoverage.Analyze(ingredientcoverage.Dataset{
		Name:  "custom",
		Label: "Custom",
		Terms: []string{
			"seedless cucumbers",
			"jalapeño peppers",
			"kiwi",
			"dill",
		},
	}, wrapIngredients(descriptions), ingredientcoverage.BaselineMatcher())
	if report.MatchedTerms != 3 {
		t.Fatalf("MatchedTerms = %d, want %d", report.MatchedTerms, 3)
	}
	if report.TotalMatches != 3 {
		t.Fatalf("TotalMatches = %d, want %d", report.TotalMatches, 3)
	}
}

func strPtr(s string) *string {
	return &s
}

func wrapIngredients(descriptions []*string) []kroger.Ingredient {
	ingredients := make([]kroger.Ingredient, 0, len(descriptions))
	for _, description := range descriptions {
		ingredients = append(ingredients, kroger.Ingredient{Description: description})
	}
	return ingredients
}
