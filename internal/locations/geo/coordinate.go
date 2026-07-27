package geo

import (
	"fmt"
	"strconv"
	"strings"
)

type Coordinate struct {
	Lat float64
	Lon float64
}

func FromString(latRaw, lonRaw string) (Coordinate, error) {
	lat, err := strconv.ParseFloat(strings.TrimSpace(latRaw), 64)
	if err != nil {
		return Coordinate{}, fmt.Errorf("invalid latitude: %q", latRaw)
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(lonRaw), 64)
	if err != nil {
		return Coordinate{}, fmt.Errorf("invalid longitude: %q", lonRaw)
	}

	coord := Coordinate{Lat: lat, Lon: lon}
	if err := coord.Valid(); err != nil {
		return Coordinate{}, err
	}
	return coord, nil
}

func (c Coordinate) Valid() error {
	if c.Lat < -90 || c.Lat > 90 {
		return fmt.Errorf("latitude %f must be between -90 and 90", c.Lat)
	}
	if c.Lon < -180 || c.Lon > 180 {
		return fmt.Errorf("longitude %f must be between -180 and 180", c.Lon)
	}

	if c.Lat == 0 && c.Lon == 0 {
		return fmt.Errorf("invalid zero coordinates: %f, %f", c.Lat, c.Lon)
	}

	return nil
}
