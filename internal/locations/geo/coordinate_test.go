package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCoordinateValid(t *testing.T) {
	tests := []struct {
		name  string
		coord Coordinate
		want  bool
	}{
		{
			name:  "valid",
			coord: Coordinate{Lat: 47.6097, Lon: -122.3331},
			want:  true,
		},
		{
			name:  "zero point",
			coord: Coordinate{},
			want:  false,
		},
		{
			name:  "latitude too low",
			coord: Coordinate{Lat: -90.1, Lon: -122.3331},
			want:  false,
		},
		{
			name:  "latitude too high",
			coord: Coordinate{Lat: 90.1, Lon: -122.3331},
			want:  false,
		},
		{
			name:  "longitude too low",
			coord: Coordinate{Lat: 47.6097, Lon: -180.1},
			want:  false,
		},
		{
			name:  "longitude too high",
			coord: Coordinate{Lat: 47.6097, Lon: 180.1},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.coord.Valid())
		})
	}
}
