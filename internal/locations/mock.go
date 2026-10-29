package locations

import (
	"context"
	"fmt"

	"careme/internal/locations/geo"

	"github.com/samber/lo"
)

type mock struct{}

func NewMock() LocationBackend { return mock{} }

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
	return &l, nil
}

func (m mock) GetLocationsByCoordinates(ctx context.Context, coordinates geo.Coordinate) ([]Location, error) {
	// Place mock stores near the search point so the aggregate distance filter keeps them.
	stores := lo.Values(fakes)
	for i := range stores {
		stores[i].Lat = new(coordinates.Lat)
		stores[i].Lon = new(coordinates.Lon)
	}
	return stores, nil
}

func (mock) IsID(locationID string) bool {
	_, ok := fakes[locationID]
	return ok
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
