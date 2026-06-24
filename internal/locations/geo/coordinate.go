package geo

type Coordinate struct {
	Lat float64
	Lon float64
}

func (c Coordinate) Valid() bool {
	return c.Lat >= -90 && c.Lat <= 90 &&
		c.Lon >= -180 && c.Lon <= 180 &&
		!(c.Lat == 0 && c.Lon == 0)
}
