package types

import (
	"time"

	"careme/internal/locations/geo"
)

type Location struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Address  string    `json:"address"`
	State    string    `json:"state"`
	ZipCode  string    `json:"zip_code"`
	Lat      *float64  `json:"lat,omitempty"`
	Lon      *float64  `json:"lon,omitempty"`
	CachedAt time.Time `json:"cached_at"`
	Chain    string    `json:"chain,omitempty"`
}

// Coordinate Helper will panic nil coordiantes so backfill first.
func (l *Location) Coordiante() geo.Coordinate {
	return geo.Coordinate{Lat: *l.Lat, Lon: *l.Lon}
}

type ZipCentroid = geo.Coordinate

// Why is this here?
type ProduceScore struct {
	Score int       `json:"score"`
	Date  time.Time `json:"date"`
}
