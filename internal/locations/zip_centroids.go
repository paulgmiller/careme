package locations

import (
	"bytes"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
	_ "embed"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
)

type zipCentroidIndex struct {
	centroids map[string]locationtypes.ZipCentroid
}

func (z zipCentroidIndex) Len() int {
	return len(z.centroids)
}

var (
	// zip_centroids.csv generation notes:
	// 1) Downloaded U.S. Census Bureau Gazetteer ZCTA centroids:
	//    https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2025_Gazetteer/2025_Gaz_zcta_national.zip
	// 2) Transformed to "zip,lat,lon" CSV with:
	//    unzip -p /tmp/2025_Gaz_zcta_national.zip 2025_Gaz_zcta_national.txt \
	//      | awk -F '|' 'BEGIN{print "zip,lat,lon"} NR>1 && $1 != "" {printf "%s,%s,%s\n", $1, $7, $8}' \
	//      > internal/locations/zip_centroids.csv
	// 3) Committed resulting dataset to this repository.
	//
	//go:embed zip_centroids.csv
	zipCentroidsCSV []byte

	// zip_centroids_backfill.csv contains ZIP centroids missing from the Census
	// ZCTA dataset and backfilled from:
	// https://raw.githubusercontent.com/millbj92/US-Zip-Codes-JSON/refs/heads/master/USCities.json
	//
	//go:embed zip_centroids_backfill.csv
	zipCentroidsBackfillCSV []byte
)

func LoadCentroids() zipCentroidIndex {
	centroids, err := parseZipCentroidsCSV(zipCentroidsCSV)
	if err != nil {
		panic("failed to parse embedded zip centroids dataset: " + err.Error())
	}
	backfill, err := parseZipCentroidsCSV(zipCentroidsBackfillCSV)
	if err != nil {
		panic("failed to parse embedded zip centroid backfill dataset: " + err.Error())
	}
	for zip, centroid := range backfill {
		if _, ok := centroids[zip]; ok {
			continue
		}
		centroids[zip] = centroid
	}
	return zipCentroidIndex{centroids: centroids}
}

func parseZipCentroidsCSV(raw []byte) (map[string]locationtypes.ZipCentroid, error) {
	reader := csv.NewReader(bytes.NewReader(raw))
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("empty centroid dataset")
	}

	data := make(map[string]locationtypes.ZipCentroid, len(rows)-1)
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 3 {
			continue
		}
		lat, err := strconv.ParseFloat(row[1], 64)
		if err != nil {
			continue
		}
		lon, err := strconv.ParseFloat(row[2], 64)
		if err != nil {
			continue
		}
		data[row[0]] = locationtypes.ZipCentroid{Lat: lat, Lon: lon}
	}
	return data, nil
}

func (z zipCentroidIndex) ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool) {
	zip5, ok := normalizeZIP(zip)
	if !ok {
		return locationtypes.ZipCentroid{}, false
	}

	centroid, ok := z.centroids[zip5]
	return centroid, ok
}

func (z zipCentroidIndex) NearestZIPToCoordinates(lat, lon float64) (string, bool) {
	if len(z.centroids) == 0 {
		return "", false
	}

	nearestZip := ""
	nearestDistance := 0.0
	for zip, centroid := range z.centroids {
		distance := geo.HaversineMiles(lat, lon, centroid.Lat, centroid.Lon)
		if nearestZip == "" || distance < nearestDistance {
			nearestZip = zip
			nearestDistance = distance
		}
	}

	return nearestZip, nearestZip != ""
}

func normalizeZIP(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 5 && isAllDigits(raw) {
		return raw, true
	}
	if len(raw) == 10 && raw[5] == '-' && isAllDigits(raw[:5]) && isAllDigits(raw[6:]) {
		return raw[:5], true
	}
	return "", false
}

func isAllDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
