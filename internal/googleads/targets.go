package googleads

import (
	"math"
)

type Target struct {
	StoreID     string
	StoreName   string
	Address     string
	LatMicro    int64
	LonMicro    int64
	RadiusMiles float64
	FinalURL    string
}

type Plan struct {
	Create []Target
	Skip   []Target
}

func MicroDegrees(v float64) int64 {
	return int64(math.Round(v * 1_000_000))
}
