package main

import (
	"reflect"
	"testing"
)

func TestExtractZipCodes_UsesSecondColumn(t *testing.T) {
	records := [][]string{
		{"Seattle", "98032", "ignore"},
		{"Boston", "02169-1234", "ignore"},
		{"Duplicate", "98032", "ignore"},
		{"Bad", "not-a-zip", "ignore"},
	}

	got, err := extractZipCodes(records)
	if err != nil {
		t.Fatalf("extractZipCodes returned error: %v", err)
	}

	want := []string{"98032", "02169"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected zip codes: got=%v want=%v", got, want)
	}
}

func TestExtractZipCodes_SkipsRowsMissingSecondColumn(t *testing.T) {
	records := [][]string{
		{"only-one-column"},
		{"Seattle", "98032"},
		{},
		{"Boston", "02169"},
	}

	got, err := extractZipCodes(records)
	if err != nil {
		t.Fatalf("extractZipCodes returned error: %v", err)
	}

	want := []string{"98032", "02169"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected zip codes: got=%v want=%v", got, want)
	}
}

func TestNormalizeZipCode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "five digits", input: "98032", want: "98032", ok: true},
		{name: "zip plus four", input: "02169-1234", want: "02169", ok: true},
		{name: "leading and trailing spaces", input: " 98032 ", want: "98032", ok: true},
		{name: "non-digit content", input: "zip 98032", want: "", ok: false},
		{name: "too short", input: "9803", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeZipCode(tt.input)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("normalizeZipCode(%q) = (%q, %t), want (%q, %t)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}
