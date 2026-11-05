package imperfectfoods

import (
	"context"
	"fmt"

	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

type locationBackend struct{ identityProvider }

func NewLocationBackend() locationBackend { return locationBackend{} }

func (b locationBackend) HasInventory(id string) bool {
	if !b.IsID(id) {
		return false
	}
	_, err := parseSnapshot(snapshotJSON)
	return err == nil
}

func (b locationBackend) GetLocationByID(ctx context.Context, id string) (*locationtypes.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !b.IsID(id) {
		return nil, fmt.Errorf("invalid Imperfect Foods location ID %q", id)
	}
	// Census ZIP 98005 centroid from the shared zip_centroids.csv dataset.
	// This is a demo reference point, not a verified delivery area or home address.
	lat, lon := 47.612771, -122.167621
	return &locationtypes.Location{
		ID: locationID, Name: "Imperfect Foods demo", Chain: "Imperfect Foods",
		Address: "Catalog snapshot demo", ZipCode: "98005", State: "WA",
		URL: baseURL + "/shop", Lat: &lat, Lon: &lon,
	}, nil
}

func (b locationBackend) GetLocationsByCoordinates(ctx context.Context, coordinate geo.Coordinate) ([]locationtypes.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := coordinate.Valid(); err != nil {
		return nil, err
	}
	location, err := b.GetLocationByID(ctx, locationID)
	if err != nil {
		return nil, err
	}
	// The shared nearby-store filter limits discovery around the 98005 demo point.
	return []locationtypes.Location{*location}, nil
}
