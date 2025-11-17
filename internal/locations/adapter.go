package locations

import "context"

// LocationAdapter provides a simplified interface for getting location details
type LocationAdapter struct {
	server *locationServer
}

// NewLocationAdapter creates a new adapter
func NewLocationAdapter(server *locationServer) *LocationAdapter {
	return &LocationAdapter{server: server}
}

// GetLocationNameByID retrieves just the location name
func (a *LocationAdapter) GetLocationNameByID(ctx context.Context, locationID string) (string, error) {
	loc, err := a.server.GetLocationByID(ctx, locationID)
	if err != nil {
		return "", err
	}
	return loc.Name, nil
}
