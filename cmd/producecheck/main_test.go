package main

import (
	"careme/internal/kroger"
	"reflect"
	"testing"
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
	descriptions := []*string{
		strPtr("Fresh Seedless Mini Cucumbers"),
		strPtr("Fresh Jalapeno Peppers"),
		strPtr("Simple Truth Organic® Whole Baby Bella Mushrooms"),
		strPtr("Simple Truth Organic® Little Gem Butter Crunch Salad Mix"),
		strPtr("Simple Truth Organic® Kiwifruit"),
	}
	ingredients := make([]kroger.Ingredient, 0, len(descriptions))
	for _, d := range descriptions {
		ingredients = append(ingredients, kroger.Ingredient{Description: d})
	}

	tests := []struct {
		term string
		want int
	}{
		{term: "seedless cucumbers", want: 1},
		{term: "jalapeño peppers", want: 1},
		{term: "baby bella (cremini) mushrooms", want: 1},
		{term: "little gem lettuce", want: 1},
		{term: "kiwi", want: 1},
	}
	for _, tc := range tests {
		got := hasProduce(ingredients, tc.term)
		if len(got) != tc.want {
			t.Fatalf("hasProduce(%q) = %d matches, want %d", tc.term, len(got), tc.want)
		}
	}
}

func strPtr(s string) *string {
	return &s
}
