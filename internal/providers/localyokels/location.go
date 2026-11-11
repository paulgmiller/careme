package localyokels

import (
	"context"
	"fmt"

	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

type locationBackend struct{ identityProvider }

func NewLocationBackend() locationBackend             { return locationBackend{} }
func (b locationBackend) HasInventory(id string) bool { return b.IsID(id) }
func (b locationBackend) GetLocationByID(ctx context.Context, id string) (*locationtypes.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !b.IsID(id) {
		return nil, fmt.Errorf("invalid Local Yokels location ID %q", id)
	}
	// 8416 5th Ave NE, Seattle, WA 98115.
	lat, lon := 47.6900, -122.3227
	return &locationtypes.Location{ID: locationID, Name: "Local Yokels Market", Chain: "Local Yokels", Address: "8416 5th Ave NE, Seattle, WA 98115", State: "WA", ZipCode: "98115", URL: baseURL + "/Products", Lat: &lat, Lon: &lon}, nil
}

func (b locationBackend) GetLocationsByCoordinates(ctx context.Context, coordinate geo.Coordinate) ([]locationtypes.Location, error) {
	if err := coordinate.Valid(); err != nil {
		return nil, err
	}
	location, err := b.GetLocationByID(ctx, locationID)
	if err != nil {
		return nil, err
	}
	// The shared nearby-store filter applies distance to this fixed storefront.
	// TODO: Support a wider discovery radius for Local Yokels' delivery area.
	return []locationtypes.Location{*location}, nil
}
