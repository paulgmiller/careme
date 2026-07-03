package geo

import (
	"errors"
	"strconv"
	"strings"
)

var (
	ErrInvalidLatitude   = errors.New("invalid latitude")
	ErrInvalidLongitude  = errors.New("invalid longitude")
	ErrInvalidCoordinate = errors.New("invalid coordinate")
)

type Coordinate struct {
	Lat float64
	Lon float64
}

func FromString(latRaw, lonRaw string) (Coordinate, error) {
	lat, err := strconv.ParseFloat(strings.TrimSpace(latRaw), 64)
	if err != nil {
		return Coordinate{}, ErrInvalidLatitude
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(lonRaw), 64)
	if err != nil {
		return Coordinate{}, ErrInvalidLongitude
	}

	coord := Coordinate{Lat: lat, Lon: lon}
	if !coord.Valid() {
		return Coordinate{}, ErrInvalidCoordinate
	}
	return coord, nil
}

func (c Coordinate) Valid() bool {
	return c.Lat >= -90 && c.Lat <= 90 &&
		c.Lon >= -180 && c.Lon <= 180 &&
		!(c.Lat == 0 && c.Lon == 0)
}
