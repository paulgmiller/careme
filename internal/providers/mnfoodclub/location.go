package mnfoodclub

import (
	"context"
	"fmt"

	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

// Delivery location IDs are shared by provider routing and campaigns.
const (
	MNFoodClubLocationID      = "mnfoodclub_delivery"
	TCFarmLocationID          = "tcfarm_delivery"
	NelsonGrassFarmLocationID = "nelsongrassfarm_delivery"
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

type storefront struct {
	id   string
	name string
	url  string
}

var storefronts = [...]storefront{
	{id: MNFoodClubLocationID, name: "MN Food Club", url: "https://mnfood.club/"},
	{id: TCFarmLocationID, name: "TC Farm", url: "https://tc.farm/"},
	{id: NelsonGrassFarmLocationID, name: "Nelson Grass Farm", url: "https://nelsongrassfarm.com/"},
}

// locationBackend exposes MNFoodClub home delivery within the approximate area.
type locationBackend struct{ identityProvider }

func NewLocationBackend() locationBackend { return locationBackend{} }

// CachePolicy disables location caching because search coordinates represent
// the current delivery point rather than a fixed store.
func (locationBackend) CachePolicy() (version string, cacheable bool) { return "", false }

func (b locationBackend) HasInventory(locationID string) bool {
	_, err := b.GetLocationByID(context.Background(), locationID)
	return err == nil
}

func (b locationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 10035 Flanders Court NE
	// Blaine, MN 55449
	coordinates := geo.Coordinate{Lat: 45.152458660615245, Lon: -93.19369341504489}
	for _, store := range storefronts {
		if locationID == store.id {
			return store.deliveryLocation(coordinates), nil
		}
	}
	return nil, fmt.Errorf("invalid MNFoodClub location ID %q", locationID)
}

func (locationBackend) GetLocationsByCoordinates(ctx context.Context, coordinates geo.Coordinate) ([]locationtypes.Location, error) {
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
	locations := make([]locationtypes.Location, 0, len(storefronts))
	for _, store := range storefronts {
		locations = append(locations, *store.deliveryLocation(coordinates))
	}
	return locations, nil
}

func inDeliveryArea(coordinates geo.Coordinate) bool {
	return coordinates.Lat >= deliverySouth && coordinates.Lat <= deliveryNorth &&
		coordinates.Lon >= deliveryWest && coordinates.Lon <= deliveryEast
}

func (s storefront) deliveryLocation(coordinates geo.Coordinate) *locationtypes.Location {
	return &locationtypes.Location{
		ID: s.id, Name: s.name + " delivery", Chain: s.name, Address: "Home delivery", URL: s.url,
		Lat: &coordinates.Lat, Lon: &coordinates.Lon,
	}
}
