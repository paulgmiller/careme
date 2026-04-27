package kroger

import (
	"context"
	"fmt"

	"careme/internal/config"
	krogerlocations "careme/internal/kroger/locations"
	locationtypes "careme/internal/locations/types"
)

const chainName = "kroger"

type locationClient interface {
	SearchLocationsWithResponse(ctx context.Context, params *krogerlocations.SearchLocationsParams, reqEditors ...krogerlocations.RequestEditorFn) (*krogerlocations.SearchLocationsResponse, error)
	LocationsGetByIDWithResponse(ctx context.Context, locationId string, reqEditors ...krogerlocations.RequestEditorFn) (*krogerlocations.LocationsGetByIDResponse, error)
}

type LocationBackend struct {
	client locationClient
}

func NewLocationBackendFromConfig(cfg *config.Config) (*LocationBackend, error) {
	requestEditor := newBearerTokenRequestEditor(cfg)
	client, err := krogerlocations.NewClientWithResponses("https://api.kroger.com",
		krogerlocations.WithRequestEditorFn(krogerlocations.RequestEditorFn(requestEditor)),
	)
	if err != nil {
		return nil, fmt.Errorf("create kroger locations client: %w", err)
	}
	return &LocationBackend{client: client}, nil
}

func (b *LocationBackend) IsID(locationID string) bool {
	if locationID == "" {
		return false
	}
	for i := 0; i < len(locationID); i++ {
		if locationID[i] < '0' || locationID[i] > '9' {
			return false
		}
	}
	return true
}

func (*LocationBackend) HasInventory(locationID string) bool {
	return true
}

func (b *LocationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	resp, err := b.client.LocationsGetByIDWithResponse(ctx, locationID)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil || resp.JSON200.Data == nil {
		return nil, fmt.Errorf("no data found for location ID %s", locationID)
	}

	data := resp.JSON200.Data
	address := ""
	state := ""
	zipCode := ""
	if data.Address != nil {
		address = stringValue(data.Address.AddressLine1)
		state = stringValue(data.Address.State)
		zipCode = stringValue(data.Address.ZipCode)
	}
	var lat *float64
	var lon *float64
	if data.Geolocation != nil {
		lat = float32PtrToFloat64Ptr(data.Geolocation.Latitude)
		lon = float32PtrToFloat64Ptr(data.Geolocation.Longitude)
	}

	return &locationtypes.Location{
		ID:      locationID,
		Name:    stringValue(data.Name),
		Address: address,
		State:   state,
		ZipCode: zipCode,
		Lat:     lat,
		Lon:     lon,
		Chain:   chainName,
	}, nil
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	params := &krogerlocations.SearchLocationsParams{
		FilterZipCodeNear: &zipcode,
	}
	resp, err := b.client.SearchLocationsWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil || resp.JSON200.Data == nil {
		return nil, nil
	}

	locations := make([]locationtypes.Location, 0, len(*resp.JSON200.Data))
	for _, locationData := range *resp.JSON200.Data {
		address := ""
		state := ""
		zipCode := ""
		if locationData.Address != nil {
			address = stringValue(locationData.Address.AddressLine1)
			state = stringValue(locationData.Address.State)
			zipCode = stringValue(locationData.Address.ZipCode)
		}
		var lat *float64
		var lon *float64
		if locationData.Geolocation != nil {
			lat = float32PtrToFloat64Ptr(locationData.Geolocation.Latitude)
			lon = float32PtrToFloat64Ptr(locationData.Geolocation.Longitude)
		}

		locations = append(locations, locationtypes.Location{
			ID:      stringValue(locationData.LocationId),
			Name:    stringValue(locationData.Name),
			Address: address,
			State:   state,
			ZipCode: zipCode,
			Lat:     lat,
			Lon:     lon,
			Chain:   chainName,
		})
	}
	return locations, nil
}

func stringValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func float32PtrToFloat64Ptr(p *float32) *float64 {
	if p == nil {
		return nil
	}
	v := float64(*p)
	return &v
}
