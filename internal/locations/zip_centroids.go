package locations

import (
	"bytes"
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
)

func LoadCentroids() zipCentroidIndex {
	zci, err := parseZipCentroidsCSV(zipCentroidsCSV)
	if err != nil {
		panic("failed to parse embedded zip centroids dataset: " + err.Error())
	}
	return zci
}

func parseZipCentroidsCSV(raw []byte) (zipCentroidIndex, error) {
	reader := csv.NewReader(bytes.NewReader(raw))
	rows, err := reader.ReadAll()
	if err != nil {
		return zipCentroidIndex{}, err
	}
	if len(rows) == 0 {
		return zipCentroidIndex{}, errors.New("empty centroid dataset")
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
	return zipCentroidIndex{centroids: data}, nil
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
		distance := haversineMiles(lat, lon, centroid.Lat, centroid.Lon)
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
