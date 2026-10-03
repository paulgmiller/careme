package locations

import (
	"context"
	"fmt"
	"html/template"
	"net/http"

	"careme/internal/auth"
	"careme/internal/locations/geo"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"

	"github.com/samber/lo"
)

type mock struct{ signature func(string) string }

func NewMock(signature func(string) string) Store { return mock{signature: signature} }

var fakes = map[string]Location{
	"70500010": {
		ID:      "70500010",
		Name:    "Big Willys",
		Address: "1 willy ave",
		State:   "North Dakota",
		ZipCode: "58102",
		Lat:     new(46.8772),
		Lon:     new(-96.7898),
	},
	"70505000": {
		ID:      "70505000",
		Name:    "Piggly Wiggly",
		Address: "20 somewhere st",
		State:   "North Carolina",
		ZipCode: "28104",
		Lat:     new(35.0074),
		Lon:     new(-80.7381),
	},
}

func (m mock) GetLocationByID(ctx context.Context, locationID string) (*Location, error) {
	l, ok := fakes[locationID]
	if !ok {
		return nil, fmt.Errorf("no location %s", locationID)
	}
	if m.signature != nil {
		l.StaplesSignature = m.signature(l.ID)
	}
	return &l, nil
}

func (m mock) GetLocationsByCoordinates(ctx context.Context, coordinates geo.Coordinate) ([]Location, error) {
	locations := lo.Values(fakes)
	if m.signature != nil {
		for i := range locations {
			locations[i].StaplesSignature = m.signature(locations[i].ID)
		}
	}
	return locations, nil
}

func (mock) HasInventory(locationID string) bool {
	return true
}

func (mock) RequestStore(ctx context.Context, locationID string) error {
	return nil
}

func (mock) RequestedStoreIDs(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m mock) Register(mux routing.Registrar, _ auth.AuthClient) {
	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			Locations       []Location
			Zip             string
			FavoriteStore   string
			ClarityScript   template.HTML
			GoogleTagScript template.HTML
			Style           seasons.Style
			ServerSignedIn  bool
		}{
			Locations:       lo.Values(fakes),
			Zip:             r.URL.Query().Get("zip"),
			FavoriteStore:   "",
			ClarityScript:   templates.ClarityScript(r.Context()),
			GoogleTagScript: templates.GoogleTagScript(),
			Style:           seasons.GetCurrentStyle(),
			ServerSignedIn:  false,
		}
		if err := templates.Location.Execute(w, data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}
