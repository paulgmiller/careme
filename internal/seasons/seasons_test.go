package seasons

import (
	"testing"
	"time"
)

func TestGetSeason(t *testing.T) {
	tests := []struct {
		name     string
		date     time.Time
		expected Season
	}{
		{
			name:     "September is Fall",
			date:     time.Date(2024, time.September, 15, 0, 0, 0, 0, time.UTC),
			expected: Fall,
		},
		{
			name:     "October is Fall",
			date:     time.Date(2024, time.October, 15, 0, 0, 0, 0, time.UTC),
			expected: Fall,
		},
		{
			name:     "November is Fall",
			date:     time.Date(2024, time.November, 15, 0, 0, 0, 0, time.UTC),
			expected: Fall,
		},
		{
			name:     "December is Winter",
			date:     time.Date(2024, time.December, 15, 0, 0, 0, 0, time.UTC),
			expected: Winter,
		},
		{
			name:     "January is Winter",
			date:     time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC),
			expected: Winter,
		},
		{
			name:     "February is Winter",
			date:     time.Date(2024, time.February, 15, 0, 0, 0, 0, time.UTC),
			expected: Winter,
		},
		{
			name:     "March is Spring",
			date:     time.Date(2024, time.March, 15, 0, 0, 0, 0, time.UTC),
			expected: Spring,
		},
		{
			name:     "April is Spring",
			date:     time.Date(2024, time.April, 15, 0, 0, 0, 0, time.UTC),
			expected: Spring,
		},
		{
			name:     "May is Spring",
			date:     time.Date(2024, time.May, 15, 0, 0, 0, 0, time.UTC),
			expected: Spring,
		},
		{
			name:     "June is Summer",
			date:     time.Date(2024, time.June, 15, 0, 0, 0, 0, time.UTC),
			expected: Summer,
		},
		{
			name:     "July is Summer",
			date:     time.Date(2024, time.July, 15, 0, 0, 0, 0, time.UTC),
			expected: Summer,
		},
		{
			name:     "August is Summer",
			date:     time.Date(2024, time.August, 15, 0, 0, 0, 0, time.UTC),
			expected: Summer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSeason(tt.date)
			if result != tt.expected {
				t.Errorf("GetSeason(%v) = %v, want %v", tt.date, result, tt.expected)
			}
		})
	}
}

func TestGetColorScheme(t *testing.T) {
	tests := []struct {
		name   string
		season Season
	}{
		{name: "Fall colors", season: Fall},
		{name: "Winter colors", season: Winter},
		{name: "Spring colors", season: Spring},
		{name: "Summer colors", season: Summer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			colors := GetColorScheme(tt.season)
			// Just verify we got non-empty colors
			if colors.C50 == "" || colors.C500 == "" || colors.C900 == "" {
				t.Errorf("GetColorScheme(%v) returned empty colors", tt.season)
			}
		})
	}
}

func TestGetCurrentSeason(t *testing.T) {
	// Just verify it returns a valid season
	season := GetCurrentSeason()
	validSeasons := []Season{Fall, Winter, Spring, Summer}
	valid := false
	for _, s := range validSeasons {
		if season == s {
			valid = true
			break
		}
	}
	if !valid {
		t.Errorf("GetCurrentSeason() returned invalid season: %v", season)
	}
}

func TestGetCurrentColorScheme(t *testing.T) {
	// Just verify it returns non-empty colors
	colors := GetCurrentColorScheme()
	if colors.C50 == "" || colors.C500 == "" || colors.C900 == "" {
		t.Errorf("GetCurrentColorScheme() returned empty colors")
	}
}

func TestGetCurrentStyle(t *testing.T) {
	// Verify it returns a style with valid colors
	style := GetCurrentStyle()
	if style.Colors.C50 == "" || style.Colors.C500 == "" || style.Colors.C900 == "" {
		t.Errorf("GetCurrentStyle() returned empty colors")
	}
}
