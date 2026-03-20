package main

import "testing"

func TestShouldResumeSkip(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{status: "success", want: true},
		{status: "invalid_store", want: true},
		{status: "no_ad", want: false},
		{status: "scrape_error", want: false},
	}

	for _, tc := range tests {
		if got := shouldResumeSkip(tc.status); got != tc.want {
			t.Fatalf("shouldResumeSkip(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
