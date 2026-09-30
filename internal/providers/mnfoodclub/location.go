package mnfoodclub

import (
	"context"
	"fmt"

	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

// Rough rectangle around the supplied delivery map, not its exact service polygon.
// It includes St. Cloud, the Twin Cities, Hudson, and Rochester.
// https://mnfood.club/About/ see Delivery Deails.
const (
	deliverySouth = 43.90
	deliveryNorth = 45.65
	deliveryWest  = -94.30
	deliveryEast  = -92.35
)

const theLocationID = LocationIDPrefix + "delivery"

// LocationBackend exposes MNFoodClub home delivery within the approximate area.
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
		return nil, fmt.Errorf("invalid MNFoodClub location ID %q", locationID)
	}
	// 10035 Flanders Court NE
	// Blaine, MN 55449
	coordinates := geo.Coordinate{Lat: 45.152458660615245, Lon: -93.19369341504489}
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
	return coordinates.Lat >= deliverySouth && coordinates.Lat <= deliveryNorth &&
		coordinates.Lon >= deliveryWest && coordinates.Lon <= deliveryEast
}

func deliveryLocation(id string, coordinates geo.Coordinate) *locationtypes.Location {
	return &locationtypes.Location{
		ID: id, Name: "MNFoodClub delivery", Chain: "MNFoodClub", Address: "Home delivery",
		Lat: &coordinates.Lat, Lon: &coordinates.Lon,
	}
}
