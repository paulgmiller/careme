package recipes

import (
	"careme/internal/locations"
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

type staticLocationLookup struct {
	location *locations.Location
}

func (s staticLocationLookup) GetLocationByID(_ context.Context, _ string) (*locations.Location, error) {
	return s.location, nil
}

func TestDefaultRecipeDate_Uses9AMStoreBoundary(t *testing.T) {
	storeLoc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load timezone: %v", err)
	}

	beforeBoundary := time.Date(2026, 1, 15, 13, 59, 0, 0, time.UTC) // 08:59 in New York
	before := defaultRecipeDate(beforeBoundary, storeLoc)
	if got, want := before.Format("2006-01-02"), "2026-01-14"; got != want {
		t.Fatalf("expected previous day before 9AM boundary, got %s", got)
	}

	atBoundary := time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC) // 09:00 in New York
	after := defaultRecipeDate(atBoundary, storeLoc)
	if got, want := after.Format("2006-01-02"), "2026-01-15"; got != want {
		t.Fatalf("expected same day at 9AM boundary, got %s", got)
	}
}

func TestParseQueryArgs_DefaultDateUsesStoreZipHeuristic(t *testing.T) {
	oldNowFn := nowFn
	nowFn = func() time.Time {
		return time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC) // 05:30 in New York
	}
	defer func() {
		nowFn = oldNowFn
	}()

	s := &server{
		locServer: staticLocationLookup{
			location: &locations.Location{
				ID:      "store-1",
				Name:    "Test Store",
				ZipCode: "10001",
			},
		},
	}

	req := httptest.NewRequest("GET", "/recipes?location=store-1", nil)
	p, err := s.ParseQueryArgs(context.Background(), req)
	if err != nil {
		t.Fatalf("ParseQueryArgs returned error: %v", err)
	}

	if got, want := p.Date.Format("2006-01-02"), "2026-01-14"; got != want {
		t.Fatalf("expected default date %s, got %s", want, got)
	}
	if got, want := p.Date.Location().String(), "America/New_York"; got != want {
		t.Fatalf("expected date location %s, got %s", want, got)
	}
}

func TestTimezoneNameForZip(t *testing.T) {
	cases := []struct {
		zip      string
		wantName string
		wantOK   bool
	}{
		{zip: "10001", wantName: "America/New_York", wantOK: true},
		{zip: "60601", wantName: "America/Chicago", wantOK: true},
		{zip: "80202", wantName: "America/Denver", wantOK: true},
		{zip: "94105", wantName: "America/Los_Angeles", wantOK: true},
		{zip: "abcde", wantName: "", wantOK: false},
	}
	for _, tc := range cases {
		gotName, gotOK := timezoneNameForZip(tc.zip)
		if gotName != tc.wantName || gotOK != tc.wantOK {
			t.Fatalf("zip %q: got (%q,%t), want (%q,%t)", tc.zip, gotName, gotOK, tc.wantName, tc.wantOK)
		}
	}
}
