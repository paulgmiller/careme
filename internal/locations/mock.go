package locations

import (
	"careme/internal/auth"
	"careme/internal/seasons"
	"careme/internal/templates"
	"context"
	"fmt"
	"html/template"
	"net/http"

	"github.com/samber/lo"
)

type mock struct{}

var fakes = map[string]Location{
	"10": {
		ID:      "10",
		Name:    "Big Willys",
		Address: "1 willy ave",
		State:   "North Dakota",
	},
	"5000": {
		ID:      "5000",
		Name:    "Piggly Wiggly",
		Address: "20 somewhere st",
		State:   "North Carolina",
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

func (m mock) Register(mux *http.ServeMux, _ auth.AuthClient) {
	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			Locations     []Location
			Zip           string
			FavoriteStore string
			ClarityScript template.HTML
			Style         seasons.Style
		}{
			Locations:     lo.Values(fakes),
			Zip:           r.URL.Query().Get("zip"),
			FavoriteStore: "",
			ClarityScript: templates.ClarityScript(),
			Style:         seasons.GetCurrentStyle(),
		}
		if err := templates.Location.Execute(w, data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}
