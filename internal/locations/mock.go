package locations

import (
	"context"
	"fmt"

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
