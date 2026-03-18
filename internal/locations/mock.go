package locations

import (
	"context"
	"fmt"
	"html/template"
	"net/http"

	"careme/internal/auth"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"

	"github.com/samber/lo"
)

type mock struct{}

var fakes = map[string]Location{
	"10": {
		ID:      "10",
		Name:    "Big Willys",
		Address: "1 willy ave",
		State:   "North Dakota",
		ZipCode: "58102",
	},
	"5000": {
		ID:      "5000",
		Name:    "Piggly Wiggly",
		Address: "20 somewhere st",
		State:   "North Carolina",
		ZipCode: "28104",
	},
}

func (m mock) GetLocationByID(ctx context.Context, locationID string) (*Location, error) {
	l, ok := fakes[locationID]
	if !ok {
		return nil, fmt.Errorf("no location %s", locationID)
	}
	return &l, nil
}

func (m mock) GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error) {
	return lo.Values(fakes), nil
}

func (m mock) NearestZIPToCoordinates(lat, lon float64) (string, bool) {
	for _, location := range fakes {
		return location.ZipCode, true
	}
	return "", false
}

func (mock) HasInventory(locationID string) bool {
	return true
}

func (mock) RequestStore(ctx context.Context, locationID string) error {
	return nil
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
		}{
			Locations:       lo.Values(fakes),
			Zip:             r.URL.Query().Get("zip"),
			FavoriteStore:   "",
			ClarityScript:   templates.ClarityScript(r.Context()),
			GoogleTagScript: templates.GoogleTagScript(),
			Style:           seasons.GetCurrentStyle(),
		}
		if err := templates.Location.Execute(w, data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}
