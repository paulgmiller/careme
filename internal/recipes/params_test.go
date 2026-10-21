package recipes

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticLocationLookup struct {
	location *locations.Location
}

func TestDefaultParamsMNFoodClubUsesMondayForWholeWeek(t *testing.T) {
	t.Parallel()
	zone, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	for _, id := range []string{"mnfoodclub_delivery", "mnfoodclub_tcfarm_delivery", "mnfoodclub_nelsongrassfarm_delivery"} {
		t.Run(id, func(t *testing.T) {
			location := &locations.Location{ID: id}
			// Include month/year boundaries and both daylight saving transitions.
			for _, monday := range []time.Time{
				time.Date(2026, 10, 19, 0, 0, 0, 0, zone),
				time.Date(2025, 12, 29, 0, 0, 0, 0, zone),
				time.Date(2026, 3, 2, 0, 0, 0, 0, zone),
				time.Date(2026, 10, 26, 0, 0, 0, 0, zone),
			} {
				base := DefaultParams(location, monday)
				for day := range 7 {
					p := DefaultParams(location, monday.AddDate(0, 0, day).Add(12*time.Hour))
					assert.True(t, monday.Equal(p.Date), "input date %s", monday.AddDate(0, 0, day))
					assert.Equal(t, base.Hash(), p.Hash())
					assert.Equal(t, base.LocationHash(), p.LocationHash())
				}
				assert.NotEqual(t, base.Hash(), DefaultParams(location, monday.AddDate(0, 0, 7)).Hash())
			}
		})
	}
}

func TestDefaultParamsOtherStoresKeepDailyDates(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"70100023", "wholefoods_10216", "smithbrothersfarms_delivery"} {
		location := &locations.Location{ID: id}
		monday := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
		for day := range 7 {
			date := monday.AddDate(0, 0, day)
			p := DefaultParams(location, date.Add(12*time.Hour))
			assert.Equal(t, date, p.Date)
			assert.NotEqual(t, p.Hash(), DefaultParams(location, date.AddDate(0, 0, 1)).Hash())
		}
	}
}

func TestParseGenerationFormMNFoodClubWeekStartsMonday9AM(t *testing.T) {
	previousNow := nowFn
	t.Cleanup(func() { nowFn = previousNow })
	lat, lon := 45.152, -93.194
	location := &locations.Location{ID: "mnfoodclub_delivery", Lat: &lat, Lon: &lon}
	for _, tt := range []struct{ now, monday string }{
		{"2026-10-19T13:59:00Z", "2026-10-12"},
		{"2026-10-19T14:00:00Z", "2026-10-19"},
		{"2026-10-25T23:59:00Z", "2026-10-19"},
		{"2026-10-26T13:59:00Z", "2026-10-19"},
		{"2026-10-26T14:00:00Z", "2026-10-26"},
	} {
		now, err := time.Parse(time.RFC3339, tt.now)
		require.NoError(t, err)
		nowFn = func() time.Time { return now }
		p, err := ParseGenerationForm(t.Context(), httptest.NewRequest("GET", "/recipes?location="+location.ID, nil), staticLocationLookup{location: location})
		require.NoError(t, err)
		assert.Equal(t, tt.monday, p.Date.Format(time.DateOnly))
		assert.Equal(t, "America/Chicago", p.Date.Location().String())
	}
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
	// Keep sequential: this test changes process-wide state.
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
	t.Parallel()
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
