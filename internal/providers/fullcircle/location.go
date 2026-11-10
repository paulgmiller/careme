package fullcircle

import (
	"context"
	"fmt"

	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

// Approximate Seattle metro discovery area, including Bellevue and Issaquah.
// This box is not an exact delivery service boundary.
const (
	deliverySouth = 47.30
	deliveryNorth = 47.85
	deliveryWest  = -122.50
	deliveryEast  = -121.90
)

type locationBackend struct{ identityProvider }

func NewLocationBackend() locationBackend { return locationBackend{} }

// CachePolicy disables caching because delivery results use the search point.
func (locationBackend) CachePolicy() (version string, cacheable bool) { return "", false }

func (b locationBackend) HasInventory(id string) bool { return b.IsID(id) }

func (b locationBackend) GetLocationByID(ctx context.Context, id string) (*locationtypes.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !b.IsID(id) {
		return nil, fmt.Errorf("invalid Full Circle location ID %q", id)
	}
	// Bellevue reference point for direct lookups of the delivery location.
	return deliveryLocation(geo.Coordinate{Lat: 47.61, Lon: -122.20}), nil
}

func (locationBackend) GetLocationsByCoordinates(ctx context.Context, coordinates geo.Coordinate) ([]locationtypes.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := coordinates.Valid(); err != nil {
		return nil, err
	}
	if coordinates.Lat < deliverySouth || coordinates.Lat > deliveryNorth || coordinates.Lon < deliveryWest || coordinates.Lon > deliveryEast {
		return nil, nil
	}
	// Place home delivery at the search point for the shared nearby-store filter.
	return []locationtypes.Location{*deliveryLocation(coordinates)}, nil
}

func deliveryLocation(coordinates geo.Coordinate) *locationtypes.Location {
	return &locationtypes.Location{
		ID: locationID, Name: "Full Circle delivery", Chain: "Full Circle",
		Address: "Home delivery", State: "WA", URL: baseURL,
		Lat: &coordinates.Lat, Lon: &coordinates.Lon,
	}
}
