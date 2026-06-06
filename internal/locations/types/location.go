package types

import "time"

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

type ZipCentroid struct {
	Lat float64
	Lon float64
}

type ProduceScore struct {
	Score int       `json:"score"`
	Date  time.Time `json:"date"`
}
