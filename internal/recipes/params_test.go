package recipes

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/locations"
)

type staticLocationLookup struct {
	location *locations.Location
}

func (s staticLocationLookup) GetLocationByID(_ context.Context, _ string) (*locations.Location, error) {
	if s.location == nil || (s.location.Lat != nil && s.location.Lon != nil) {
		return s.location, nil
	}
	location := *s.location
	lat := 40.7128
	lon := -74.006
	location.Lat = &lat
	location.Lon = &lon
	return &location, nil
}

func TestParseGenerationForm_DefaultDateUsesStoreCoordinates(t *testing.T) {
	oldNowFn := nowFn
	nowFn = func() time.Time {
		return time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC) // 06:00 in Los Angeles, before the store-day boundary.
	}
	defer func() {
		nowFn = oldNowFn
	}()

	lat := 47.61
	lon := -122.33
	location := &locations.Location{
		ID:   "farmersmarket_1",
		Name: "Test Market",
		Lat:  &lat,
		Lon:  &lon,
	}

	req := httptest.NewRequest("GET", "/recipes?location=farmersmarket_1", nil)
	p, err := ParseGenerationForm(context.Background(), req, staticLocationLookup{location: location})
	if err != nil {
		t.Fatalf("ParseGenerationForm returned error: %v", err)
	}
	if got, want := p.Date.Format("2006-01-02"), "2026-01-14"; got != want {
		t.Fatalf("expected default date %s, got %s", want, got)
	}
	if got, want := p.Date.Location().String(), "America/Los_Angeles"; got != want {
		t.Fatalf("expected date location %s, got %s", want, got)
	}
}

func TestParseGenerationForm_CampaignInstructionsOnlyAffectsParams(t *testing.T) {
	location := &locations.Location{
		ID:      "loc-123",
		Name:    "Test Store",
		ZipCode: "10001",
	}

	req := httptest.NewRequest("GET", "/recipes?location=loc-123&instructions=make%20it%20vegetarian&help=Save%20two%20meals", nil)
	p, err := ParseGenerationForm(context.Background(), req, staticLocationLookup{location: location})
	if err != nil {
		t.Fatalf("ParseGenerationForm returned error: %v", err)
	}

	if got, want := p.Instructions, "make it vegetarian"; got != want {
		t.Fatalf("expected instructions %q, got %q", want, got)
	}

	reqWithoutHelp := httptest.NewRequest("GET", "/recipes?location=loc-123&instructions=make%20it%20vegetarian", nil)
	pWithoutHelp, err := ParseGenerationForm(context.Background(), reqWithoutHelp, staticLocationLookup{location: location})
	if err != nil {
		t.Fatalf("ParseGenerationForm without help returned error: %v", err)
	}
	if got, want := p.Hash(), pWithoutHelp.Hash(); got != want {
		t.Fatalf("help query should not influence params hash: got %q, want %q", got, want)
	}
}
