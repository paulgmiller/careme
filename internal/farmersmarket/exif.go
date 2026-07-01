package farmersmarket

import (
	"bytes"
	"fmt"

	"careme/internal/locations/geo"

	"github.com/rwcarlsen/goexif/exif"
)

type Coordinate = geo.Coordinate

func GPSFromImage(data []byte) (Coordinate, error) {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return Coordinate{}, fmt.Errorf("decode exif: %w", err)
	}
	lat, lon, err := x.LatLong()
	if err != nil {
		return Coordinate{}, fmt.Errorf("read exif gps: %w", err)
	}
	if !(Coordinate{Lat: lat, Lon: lon}).Valid() {
		return Coordinate{}, fmt.Errorf("invalid exif gps")
	}
	return Coordinate{Lat: lat, Lon: lon}, nil
}

func AverageCoordinate(coords []Coordinate) (Coordinate, error) {
	if len(coords) == 0 {
		return Coordinate{}, fmt.Errorf("at least one coordinate is required")
	}
	var lat, lon float64
	for _, coord := range coords {
		if !coord.Valid() {
			return Coordinate{}, fmt.Errorf("invalid coordinate")
		}
		lat += coord.Lat
		lon += coord.Lon
	}
	return Coordinate{
		Lat: lat / float64(len(coords)),
		Lon: lon / float64(len(coords)),
	}, nil
}
