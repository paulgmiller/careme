package kroger

import (
	locationtypes "careme/internal/locations/types"
	"context"
	"fmt"
)

func (c *ClientWithResponses) IsID(locationID string) bool {
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

func (c *ClientWithResponses) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	resp, err := c.LocationDetailsWithResponse(ctx, locationID)
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
	}, nil
}

func (c *ClientWithResponses) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	params := &LocationListParams{
		FilterZipCodeNear: &zipcode,
	}
	resp, err := c.LocationListWithResponse(ctx, params)
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
