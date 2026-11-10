package locations

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"careme/internal/locations/geo"
)

const storeDayStartHour = 9

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

func StoreToDate(ctx context.Context, now time.Time, l *Location) (time.Time, error) {
	tz, err := resolveStoreTimeLocation(ctx, l)
	if err != nil {
		return now, err
	}
	return defaultRecipeDate(now, tz), nil
}

func defaultRecipeDate(now time.Time, storeLoc *time.Location) time.Time {
	localNow := now.In(storeLoc)
	if localNow.Hour() < storeDayStartHour {
		localNow = localNow.AddDate(0, 0, -1)
	}
	return time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, storeLoc)
}
