package locations

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"careme/internal/locations/geo"
)

func resolveStoreTimeLocation(ctx context.Context, l *Location) (*time.Location, error) {
	if l == nil {
		return nil, fmt.Errorf("nil location")
	}
	if l.Lat == nil || l.Lon == nil {
		return nil, fmt.Errorf("location %s has no coordinates", l.ID)
	}
	tzName, ok := geo.TimezoneNameForCoordinates(geo.Coordinate{Lat: *l.Lat, Lon: *l.Lon})

	if !ok {
		return nil, fmt.Errorf("unable to estimate timezone for location %s", l.ID)
	}
	storeLoc, err := time.LoadLocation(tzName)
	if err != nil {
		slog.ErrorContext(ctx, "invalid estimated timezone", "location_id", l.ID, "timezone", tzName, "error", err)
		return nil, err
	}
	return storeLoc, nil
}

// StoreTime converts an instant to the store timezone without changing the instant.
func StoreTime(ctx context.Context, now time.Time, l *Location) (time.Time, error) {
	tz, err := resolveStoreTimeLocation(ctx, l)
	if err != nil {
		return now, err
	}
	return now.In(tz), nil
}
