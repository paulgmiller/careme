package locations

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"
)

type ZipCentroid struct {
	Lat float64
	Lon float64
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

func loadEmbeddedZipCentroids() (map[string]ZipCentroid, error) {
	return parseZipCentroidsCSV(zipCentroidsCSV)
}

func parseZipCentroidsCSV(raw []byte) (map[string]ZipCentroid, error) {
	reader := csv.NewReader(bytes.NewReader(raw))
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("empty centroid dataset")
	}

	data := make(map[string]ZipCentroid, len(rows)-1)
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
		data[row[0]] = ZipCentroid{Lat: lat, Lon: lon}
	}
	return data, nil
}

func zipCentroidByZIP(zip string, centroids map[string]ZipCentroid) (ZipCentroid, bool) {
	zip5, ok := normalizeZIP(zip)
	if !ok {
		return ZipCentroid{}, false
	}

	centroid, ok := centroids[zip5]
	return centroid, ok
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
