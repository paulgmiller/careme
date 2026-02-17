package main

import (
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
			want: []string{"carrots", "brussels sprouts", "kale"},
		},
		{
			name: "drops blanks",
			csv:  " ,  , apples",
			want: []string{"apples"},
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
	want := "brussels sprouts"
	if got != want {
		t.Fatalf("normalizeTerm() = %q, want %q", got, want)
	}
}
