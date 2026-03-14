package main

import (
	"bytes"
	"reflect"
	"sort"
	"strings"
	"testing"

	"careme/internal/locations"
)

func TestExtractZipCodes_UsesSecondColumn(t *testing.T) {
	records := [][]string{
		{"Seattle", "98032", "ignore"},
		{"Boston", "02169-1234", "ignore"},
		{"Duplicate Metro", "98032", "ignore"},
		{"Bad", "not-a-zip", "ignore"},
	}

	got, err := extractZipCodes(records)
	if err != nil {
		t.Fatalf("extractZipCodes returned error: %v", err)
	}

	want := []metroZipCode{
		{Metro: "Seattle", Zip: "98032"},
		{Metro: "Boston", Zip: "02169"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected metro+zip codes: got=%v want=%v", got, want)
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

	want := []metroZipCode{
		{Metro: "Seattle", Zip: "98032"},
		{Metro: "Boston", Zip: "02169"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected metro+zip codes: got=%v want=%v", got, want)
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

func TestCountStoresByChain(t *testing.T) {
	stores := []locations.Location{
		{ID: "70500874", Chain: "Kroger"},
		{ID: "70500875", Chain: " Kroger "},
		{ID: "walmart_3098"},
		{ID: "wholefoods_10216", Chain: "wholefoods"},
		{ID: "safeway_1444"},
		{ID: "mystery"},
	}

	got := countStoresByChain(metroZipCode{Metro: "Seattle", Zip: "98032"}, stores)
	want := []zipStoreCount{
		{Metro: "Seattle", Zip: "98032", Chain: "kroger", Count: 2},
		{Metro: "Seattle", Zip: "98032", Chain: "walmart", Count: 1},
		{Metro: "Seattle", Zip: "98032", Chain: "wholefoods", Count: 1},
		{Metro: "Seattle", Zip: "98032", Chain: "safeway", Count: 1},
		{Metro: "Seattle", Zip: "98032", Chain: "unknown", Count: 1},
	}

	sortZipStoreCounts(got)
	sortZipStoreCounts(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected chain counts: got=%v want=%v", got, want)
	}
}

func TestLocationChain(t *testing.T) {
	tests := []struct {
		name  string
		store locations.Location
		want  string
	}{
		{
			name:  "uses explicit chain",
			store: locations.Location{ID: "70500874", Chain: "Kroger"},
			want:  "kroger",
		},
		{
			name:  "falls back to id prefix",
			store: locations.Location{ID: "safeway_1444"},
			want:  "safeway",
		},
		{
			name:  "falls back to kroger for numeric ids",
			store: locations.Location{ID: "70500874"},
			want:  "kroger",
		},
		{
			name:  "returns unknown when no signal exists",
			store: locations.Location{ID: "mystery"},
			want:  "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := locationChain(tt.store); got != tt.want {
				t.Fatalf("locationChain(%+v) = %q, want %q", tt.store, got, tt.want)
			}
		})
	}
}

func TestWriteCSV_EscapesMetroName(t *testing.T) {
	counts := []zipStoreCount{
		{Metro: "Seattle-Tacoma-Bellevue WA, Metro", Zip: "98032", Chain: "kroger", Count: 2},
	}

	var buf bytes.Buffer
	if err := writeCSV(&buf, counts); err != nil {
		t.Fatalf("writeCSV returned error: %v", err)
	}

	want := "metro_name,zip_code,chain,store_count\n\"Seattle-Tacoma-Bellevue WA, Metro\",98032,kroger,2\n"
	if buf.String() != want {
		t.Fatalf("unexpected csv output:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteTable_PivotsChainsAndTotals(t *testing.T) {
	metroZipCodes := []metroZipCode{
		{Metro: "Seattle", Zip: "98032"},
		{Metro: "Boston", Zip: "02169"},
	}
	counts := []zipStoreCount{
		{Metro: "Seattle", Zip: "98032", Chain: "kroger", Count: 2},
		{Metro: "Seattle", Zip: "98032", Chain: "walmart", Count: 1},
		{Metro: "Boston", Zip: "02169", Chain: "wholefoods", Count: 3},
	}

	var buf bytes.Buffer
	if err := writeTable(&buf, counts, metroZipCodes); err != nil {
		t.Fatalf("writeTable returned error: %v", err)
	}

	got := strings.Split(strings.TrimSpace(buf.String()), "\n")
	want := []string{
		"metro_name  zip_code  kroger  walmart  wholefoods  total",
		"Boston      02169     0       0        3           3",
		"Seattle     98032     2       1        0           3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected table output: got=%q want=%q", got, want)
	}
}

func TestWriteMarkdownTable_PivotsChainsAndEscapesCells(t *testing.T) {
	metroZipCodes := []metroZipCode{
		{Metro: "Boston | Cambridge", Zip: "02169"},
		{Metro: "Seattle", Zip: "98032"},
	}
	counts := []zipStoreCount{
		{Metro: "Seattle", Zip: "98032", Chain: "kroger", Count: 2},
		{Metro: "Seattle", Zip: "98032", Chain: "walmart", Count: 1},
		{Metro: "Boston | Cambridge", Zip: "02169", Chain: "wholefoods", Count: 3},
	}

	var buf bytes.Buffer
	if err := writeMarkdownTable(&buf, counts, metroZipCodes); err != nil {
		t.Fatalf("writeMarkdownTable returned error: %v", err)
	}

	want := strings.Join([]string{
		"| metro_name | zip_code | kroger | walmart | wholefoods | total |",
		"| --- | --- | --- | --- | --- | --- |",
		"| Boston \\| Cambridge | 02169 | 0 | 0 | 3 | 3 |",
		"| Seattle | 98032 | 2 | 1 | 0 | 3 |",
		"",
	}, "\n")
	if buf.String() != want {
		t.Fatalf("unexpected markdown output:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteCounts_RejectsUnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	err := writeCounts(&buf, nil, nil, "json")
	if err == nil {
		t.Fatal("expected error")
	}
}

func sortZipStoreCounts(counts []zipStoreCount) {
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Metro != counts[j].Metro {
			return counts[i].Metro < counts[j].Metro
		}
		if counts[i].Zip != counts[j].Zip {
			return counts[i].Zip < counts[j].Zip
		}
		return counts[i].Chain < counts[j].Chain
	})
}
