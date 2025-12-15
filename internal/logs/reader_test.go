package logs

import (
	"testing"
	"time"
)

func TestGetDatePrefixes(t *testing.T) {
	reader := &Reader{}

	// Test single day
	since := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	until := time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC)
	prefixes := reader.getDatePrefixes(since, until)

	if len(prefixes) != 1 {
		t.Errorf("Expected 1 prefix for same day, got %d", len(prefixes))
	}

	expected := "2024/01/15/"
	if prefixes[0] != expected {
		t.Errorf("Expected prefix %s, got %s", expected, prefixes[0])
	}

	// Test multiple days
	since = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	until = time.Date(2024, 1, 17, 14, 0, 0, 0, time.UTC)
	prefixes = reader.getDatePrefixes(since, until)

	if len(prefixes) != 3 {
		t.Errorf("Expected 3 prefixes for 3 days, got %d", len(prefixes))
	}

	expectedPrefixes := []string{"2024/01/15/", "2024/01/16/", "2024/01/17/"}
	for i, expected := range expectedPrefixes {
		if prefixes[i] != expected {
			t.Errorf("Expected prefix %s at index %d, got %s", expected, i, prefixes[i])
		}
	}
}
