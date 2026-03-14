package static

import (
	"testing"

	"careme/internal/seasons"
)

func TestFaviconBySeason(t *testing.T) {
	tests := []struct {
		name   string
		season seasons.Season
		want   []byte
	}{
		{name: "fall", season: seasons.Fall, want: faviconFall},
		{name: "winter", season: seasons.Winter, want: faviconWinter},
		{name: "spring", season: seasons.Spring, want: faviconSpring},
		{name: "summer", season: seasons.Summer, want: faviconSummer},
		{name: "default falls back to fall", season: seasons.Season("unknown"), want: faviconFall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := faviconBySeason(tt.season)
			if len(got) == 0 {
				t.Fatal("favicon should not be empty")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("faviconBySeason(%q) length = %d, want %d", tt.season, len(got), len(tt.want))
			}
		})
	}
}
