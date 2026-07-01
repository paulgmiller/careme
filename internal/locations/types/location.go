package types

import (
	"time"

	"careme/internal/locations/geo"
)

type Location struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	State   string `json:"state"`
	ZipCode string `json:"zip_code"`
	// TOOD embed go.Coordinate
	Lat      *float64  `json:"lat,omitempty"`
	Lon      *float64  `json:"lon,omitempty"`
	CachedAt time.Time `json:"cached_at"`
	Chain    string    `json:"chain,omitempty"`
}

type ZipCentroid = geo.Coordinate

type ProduceScore struct {
	Score int       `json:"score"`
	Date  time.Time `json:"date"`
}
