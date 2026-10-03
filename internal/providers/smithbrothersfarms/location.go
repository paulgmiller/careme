package smithbrothersfarms

import (
	"context"
	"fmt"

	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

// Two approximate urban-corridor boxes, not exact service polygons.
// https://www.smithbrothersfarms.com/our-service-area
var deliveryBoxes = []struct{ south, north, west, east float64 }{
	{46.95, 48.05, -122.80, -121.90}, // Puget Sound
	{45.30, 45.80, -122.95, -122.35}, // Greater Portland
}

const theLocationID = LocationIDPrefix + "delivery"

// LocationBackend exposes Smith Brothers Farms home delivery within the approximate area.
type LocationBackend struct{ identityProvider }

func NewLocationBackend() LocationBackend { return LocationBackend{} }

// IsCacheable disables location caching because search coordinates represent
// the current delivery point rather than a fixed store.
func (LocationBackend) IsCacheable() bool { return false }

func (b LocationBackend) HasInventory(locationID string) bool {
	_, err := b.GetLocationByID(context.Background(), locationID)
	return err == nil
}

func (b LocationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if locationID != theLocationID {
		return nil, fmt.Errorf("invalid Smith Brothers Farms location ID %q", locationID)
	}
	// 26401 79th Ave S, Kent, WA 98032
	coordinates := geo.Coordinate{Lat: 47.365, Lon: -122.233}
	return deliveryLocation(locationID, coordinates), nil
}

func (LocationBackend) GetLocationsByCoordinates(ctx context.Context, coordinates geo.Coordinate) ([]locationtypes.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := coordinates.Valid(); err != nil {
		return nil, err
	}
	if !inDeliveryArea(coordinates) {
		return nil, nil
	}
	// Home delivery is located at the search point so the shared nearby-store
	// distance filter works across the whole area. These locations are not cached.
	return []locationtypes.Location{*deliveryLocation(theLocationID, coordinates)}, nil
}

func inDeliveryArea(coordinates geo.Coordinate) bool {
	for _, box := range deliveryBoxes {
		if coordinates.Lat >= box.south && coordinates.Lat <= box.north && coordinates.Lon >= box.west && coordinates.Lon <= box.east {
			return true
		}
	}
	return false
}

func deliveryLocation(id string, coordinates geo.Coordinate) *locationtypes.Location {
	return &locationtypes.Location{
		ID: id, Name: "Smith Brothers Farms delivery", Chain: "Smith Brothers Farms", Address: "Home delivery",
		Lat: &coordinates.Lat, Lon: &coordinates.Lon,
	}
}
