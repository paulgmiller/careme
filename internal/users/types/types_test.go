package types

import (
	"testing"
	"time"
)

func TestParseWeekday(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Weekday
		wantErr bool
	}{
		{
			name:  "sunday",
			input: "Sunday",
			want:  time.Sunday,
		},
		{
			name:  "case insensitive",
			input: "mOnDaY",
			want:  time.Monday,
		},
		{
			name:  "lowercase",
			input: "tuesday",
			want:  time.Tuesday,
		},
		{
			name:    "invalid",
			input:   "Caturday",
			want:    time.Sunday,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWeekday(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseWeekday(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWeekday(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("parseWeekday(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
