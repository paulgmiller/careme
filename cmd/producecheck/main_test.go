package main

import (
	"reflect"
	"testing"

	"careme/internal/ai"
)

func TestParseProduceList(t *testing.T) {
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
			got := parseProduceList(tc.csv)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseProduceList() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestNormalizeTerm(t *testing.T) {
	got := normalizeTerm("  Brussel   Sprouts ")
	want := "brussels sprout"
	if got != want {
		t.Fatalf("normalizeTerm() = %q, want %q", got, want)
	}
}

func TestNormalizeTerm_RemovesParentheticalAndDiacritics(t *testing.T) {
	got := normalizeTerm(" Green Onions (Scallions), Jalapeño Peppers ")
	want := "green onion jalapeno pepper"
	if got != want {
		t.Fatalf("normalizeTerm() = %q, want %q", got, want)
	}
}

func TestHasProduce_UsesTokenMatching(t *testing.T) {
	descriptions := []string{
		"Fresh Seedless Mini Cucumbers",
		"Fresh Jalapeno Peppers",
		"Simple Truth Organic® Whole Baby Bella Mushrooms",
		"Simple Truth Organic® Kiwifruit",
	}
	ingredients := make([]ai.InputIngredient, 0, len(descriptions))
	for _, d := range descriptions {
		ingredients = append(ingredients, ai.InputIngredient{Description: d})
	}

	tests := []struct {
		term string
		want int
	}{
		{term: "seedless cucumbers", want: 1},
		{term: "jalapeño peppers", want: 1},
		{term: "baby bella (cremini) mushrooms", want: 1},
		{term: "kiwi", want: 1},
	}
	for _, tc := range tests {
		got := hasProduce(ingredients, tc.term)
		if len(got) != tc.want {
			t.Fatalf("hasProduce(%q) = %d matches, want %d", tc.term, len(got), tc.want)
		}
	}
}

func TestSummarizeFilterMatches(t *testing.T) {
	descriptions := []string{
		"Fresh Seedless Mini Cucumbers",
		"Fresh Mini Cucumbers",
		"Fresh Jalapeno Peppers",
		"Simple Truth Organic® Kiwifruit",
	}
	ingredients := make([]ai.InputIngredient, 0, len(descriptions))
	for _, d := range descriptions {
		ingredients = append(ingredients, ai.InputIngredient{Description: d})
	}

	produce := []string{
		"seedless cucumbers",
		"jalapeño peppers",
		"kiwi",
		"dill",
	}

	matchedTerms, matchedProducts := summarizeFilterMatches(produce, ingredients)
	if matchedTerms != 3 {
		t.Fatalf("summarizeFilterMatches() matchedTerms = %d, want %d", matchedTerms, 3)
	}
	if matchedProducts != 3 {
		t.Fatalf("summarizeFilterMatches() matchedProducts = %d, want %d", matchedProducts, 3)
	}
}

/*func TestAnnotateUniqueOnlyMatches(t *testing.T) {
	stats := []produceFilterStats{
		{
			FilterTerm:          "fresh produce",
			matchedDescriptions: []string{"A", "B", "C"},
		},
		{
			FilterTerm:          "mushrooms produce",
			matchedDescriptions: []string{"B", "D"},
		},
		{
			FilterTerm:          "fresh peppers",
			matchedDescriptions: []string{"E"},
		},
	}

	annotateUniqueOnlyMatches(stats)

	if stats[0].UniqueOnlyMatches != 2 {
		t.Fatalf("stats[0].UniqueOnlyMatches = %d, want %d", stats[0].UniqueOnlyMatches, 2)
	}
	if stats[1].UniqueOnlyMatches != 1 {
		t.Fatalf("stats[1].UniqueOnlyMatches = %d, want %d", stats[1].UniqueOnlyMatches, 1)
	}
	if stats[2].UniqueOnlyMatches != 1 {
		t.Fatalf("stats[2].UniqueOnlyMatches = %d, want %d", stats[2].UniqueOnlyMatches, 1)
	}
}*/
