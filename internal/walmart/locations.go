package walmart

import (
	locationtypes "careme/internal/locations/types"
	"context"
	"fmt"
	"strconv"
)

func (c *Client) GetLocationByID(_ context.Context, locationID string) (*locationtypes.Location, error) {
	//depending on cache to protect us.
	return nil, fmt.Errorf("walmart GetLocationByID not supported yet for ID %s", locationID)
}

func (c *Client) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	stores, err := c.SearchStoresByZIP(ctx, zipcode)
	if err != nil {
		return nil, err
	}

	locations := make([]locationtypes.Location, 0, len(stores))
	for _, store := range stores {
		locations = append(locations, storeToLocation(store))
	}
	return locations, nil
}

func storeToLocation(store Store) locationtypes.Location {
	return locationtypes.Location{
		ID:      strconv.Itoa(store.No),
		Name:    store.Name,
		Address: store.StreetAddress,
		State:   store.StateProvCode,
		ZipCode: store.Zip,
	}
}
