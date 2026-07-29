package walmart

import (
	"context"
	"fmt"
	"strconv"

	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

func (c *Client) GetLocationByID(_ context.Context, locationID string) (*locationtypes.Location, error) {
	// depending on cache to protect us.
	return nil, fmt.Errorf("walmart GetLocationByID not supported yet for ID %s", locationID)
}

func (c *Client) GetLocationsByCoordinates(context.Context, geo.Coordinate) ([]locationtypes.Location, error) {
	return nil, nil
}

func storeToLocation(store Store) locationtypes.Location {
	lat := store.Coordinates.Latitude
	lon := store.Coordinates.Longitude
	return locationtypes.Location{
		ID:      "walmart_" + strconv.Itoa(store.No),
		Name:    "Walmart " + store.Name,
		Address: store.StreetAddress,
		State:   store.StateProvCode,
		ZipCode: store.Zip,
		Lat:     &lat,
		Lon:     &lon,
		Chain:   "walmart",
	}
}
