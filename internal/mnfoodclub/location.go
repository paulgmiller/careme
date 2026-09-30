package mnfoodclub

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

// Rough rectangle around the supplied delivery map, not its exact service polygon.
// It includes St. Cloud, the Twin Cities, Hudson, and Rochester.
const (
	deliverySouth = 43.90
	deliveryNorth = 45.65
	deliveryWest  = -94.30
	deliveryEast  = -92.35
)

// LocationBackend exposes MNFoodClub home delivery within the approximate area.
type LocationBackend struct{ identityProvider }

func NewLocationBackend() LocationBackend { return LocationBackend{} }

func (b LocationBackend) HasInventory(locationID string) bool {
	_, err := b.GetLocationByID(context.Background(), locationID)
	return err == nil
}

func (b LocationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !b.IsID(locationID) {
		return nil, fmt.Errorf("invalid MNFoodClub location ID %q", locationID)
	}
	// Preserve the direct CLI ID as a Minneapolis delivery location.
	coordinates := geo.Coordinate{Lat: 44.985367, Lon: -93.270208}
	if locationID != LocationIDPrefix+"delivery" {
		lat, lon, ok := strings.Cut(strings.TrimPrefix(locationID, LocationIDPrefix), "_")
		if !ok {
			return nil, fmt.Errorf("invalid MNFoodClub delivery coordinates in %q", locationID)
		}
		var err error
		coordinates, err = geo.FromString(lat, lon)
		if err != nil {
			return nil, fmt.Errorf("MNFoodClub location %q: %w", locationID, err)
		}
	}
	if !inDeliveryArea(coordinates) {
		return nil, fmt.Errorf("MNFoodClub location %q is outside the delivery area", locationID)
	}
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
	// distance filter works across the whole area. Coordinates in the ID keep
	// cached locations stable when another search uses a different delivery point.
	id := LocationIDPrefix + strconv.FormatFloat(coordinates.Lat, 'f', -1, 64) + "_" + strconv.FormatFloat(coordinates.Lon, 'f', -1, 64)
	return []locationtypes.Location{*deliveryLocation(id, coordinates)}, nil
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
