package geo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimezoneNameForCoordinates(t *testing.T) {
	tests := []struct {
		name        string
		coordinates Coordinate
		want        string
		wantOK      bool
	}{
		{name: "Seattle", coordinates: Coordinate{Lat: 47.6062, Lon: -122.3321}, want: "America/Los_Angeles", wantOK: true},
		{name: "Phoenix", coordinates: Coordinate{Lat: 33.4484, Lon: -112.074}, want: "America/Phoenix", wantOK: true},
		{name: "Denver", coordinates: Coordinate{Lat: 39.7392, Lon: -104.9903}, want: "America/Denver", wantOK: true},
		{name: "Chicago", coordinates: Coordinate{Lat: 41.8781, Lon: -87.6298}, want: "America/Chicago", wantOK: true},
		{name: "New York", coordinates: Coordinate{Lat: 40.7128, Lon: -74.006}, want: "America/New_York", wantOK: true},
		{name: "Honolulu", coordinates: Coordinate{Lat: 21.3099, Lon: -157.8581}, want: "Pacific/Honolulu", wantOK: true},
		{name: "Anchorage", coordinates: Coordinate{Lat: 61.2181, Lon: -149.9003}, want: "America/Anchorage", wantOK: true},
		{name: "San Juan", coordinates: Coordinate{Lat: 18.4655, Lon: -66.1057}, want: "America/Puerto_Rico", wantOK: true},
		{name: "London", coordinates: Coordinate{Lat: 51.5072, Lon: -0.1276}, want: "Europe/London", wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := TimezoneNameForCoordinates(tt.coordinates)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestTimezoneNameForZip(t *testing.T) {
	cases := []struct {
		zip      string
		wantName string
		wantOK   bool
	}{
		{zip: "10001", wantName: "America/New_York", wantOK: true},
		{zip: "60601", wantName: "America/Chicago", wantOK: true},
		{zip: "80202", wantName: "America/Denver", wantOK: true},
		{zip: "94105", wantName: "America/Los_Angeles", wantOK: true},
		{zip: "abcde", wantName: "", wantOK: false},
	}
	for _, tc := range cases {
		gotName, gotOK := TimezoneNameForZip(tc.zip)
		if gotName != tc.wantName || gotOK != tc.wantOK {
			t.Fatalf("zip %q: got (%q,%t), want (%q,%t)", tc.zip, gotName, gotOK, tc.wantName, tc.wantOK)
		}
	}
}
