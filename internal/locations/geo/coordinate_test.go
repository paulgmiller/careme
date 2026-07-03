package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			assert.Equal(t, tt.want, tt.coord.Valid() == nil)
		})
	}
}

func TestFromString(t *testing.T) {
	got, err := FromString(" 47.6097 ", " -122.3331 ")

	require.NoError(t, err)
	assert.Equal(t, Coordinate{Lat: 47.6097, Lon: -122.3331}, got)
}

func TestFromStringRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		lat  string
		lon  string
	}{
		{
			name: "invalid latitude",
			lat:  "north",
			lon:  "-122.3331",
		},
		{
			name: "invalid longitude",
			lat:  "47.6097",
			lon:  "west",
		},
		{
			name: "out of range",
			lat:  "95",
			lon:  "-122.3331",
		},
		{
			name: "zero point",
			lat:  "0",
			lon:  "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FromString(tt.lat, tt.lon)

			require.Error(t, err)
		})
	}
}
