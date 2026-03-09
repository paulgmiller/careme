package wholefoods

import (
	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

const maxLocationDistanceMiles = 20.0

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type LocationBackend struct {
	zipLookup centroidByZip
	byID      map[string]locationtypes.Location
	locations []locationtypes.Location
}

func NewLocationBackend(ctx context.Context, c cache.ListCache, zipLookup centroidByZip) (*LocationBackend, error) {
	if c == nil {
		return nil, fmt.Errorf("list cache is required")
	}

	//Is this too much? should we just fetch a single blob that is all coordinates -> store ids and lazily fetch stores?
	summaries, err := LoadCachedStoreSummaries(ctx, c)
	if err != nil {
		return nil, err
	}

	locations := make([]locationtypes.Location, 0, len(summaries))
	byID := make(map[string]locationtypes.Location, len(summaries))
	for _, summary := range summaries {
		loc := StoreSummaryToLocation(*summary)
		locations = append(locations, loc)
		byID[loc.ID] = loc
	}

	return &LocationBackend{
		zipLookup: zipLookup,
		byID:      byID,
		locations: locations,
	}, nil
}

func (b *LocationBackend) IsID(locationID string) bool {
	_, ok := parseLocationID(locationID)
	return ok
}

func (b *LocationBackend) GetLocationByID(_ context.Context, locationID string) (*locationtypes.Location, error) {
	normalized, ok := parseLocationID(locationID)
	if !ok {
		return nil, fmt.Errorf("whole foods location id %q is invalid", locationID)
	}

	loc, exists := b.byID[normalized]
	if !exists {
		return nil, fmt.Errorf("whole foods location %q not found", locationID)
	}

	copy := loc
	return &copy, nil
}

func (b *LocationBackend) GetLocationsByZip(_ context.Context, zipcode string) ([]locationtypes.Location, error) {
	if b.zipLookup == nil {
		return copyLocations(b.locations), nil
	}

	centroid, ok := b.zipLookup.ZipCentroidByZIP(strings.TrimSpace(zipcode))
	if !ok {
		return copyLocations(b.locations), nil
	}

	type ranked struct {
		location locationtypes.Location
		distance float64
	}
	rankedLocations := make([]ranked, 0, len(b.locations))
	for _, loc := range b.locations {
		if loc.Lat == nil || loc.Lon == nil {
			continue
		}
		distance := haversineMiles(centroid.Lat, centroid.Lon, *loc.Lat, *loc.Lon)
		if distance > maxLocationDistanceMiles {
			continue
		}
		rankedLocations = append(rankedLocations, ranked{location: loc, distance: distance})
	}

	sort.SliceStable(rankedLocations, func(i, j int) bool {
		return rankedLocations[i].distance < rankedLocations[j].distance
	})

	out := make([]locationtypes.Location, 0, len(rankedLocations))
	for _, ranked := range rankedLocations {
		out = append(out, ranked.location)
	}
	return out, nil
}

func parseLocationID(locationID string) (string, bool) {
	if !strings.HasPrefix(locationID, LocationIDPrefix) {
		return "", false
	}

	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	if !isAllDigits(storeID) {
		return "", false
	}
	return LocationIDPrefix + storeID, true
}

func copyLocations(in []locationtypes.Location) []locationtypes.Location {
	out := make([]locationtypes.Location, len(in))
	copy(out, in)
	return out
}

func haversineMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMiles = 3958.7613
	toRadians := math.Pi / 180.0

	dLat := (lat2 - lat1) * toRadians
	dLon := (lon2 - lon1) * toRadians
	lat1Rad := lat1 * toRadians
	lat2Rad := lat2 * toRadians

	sinHalfDLat := math.Sin(dLat / 2.0)
	sinHalfDLon := math.Sin(dLon / 2.0)
	a := sinHalfDLat*sinHalfDLat + math.Cos(lat1Rad)*math.Cos(lat2Rad)*sinHalfDLon*sinHalfDLon
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return earthRadiusMiles * c
}
