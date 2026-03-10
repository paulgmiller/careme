# Location Data

`zip_centroids.csv` is the primary ZIP centroid dataset.

Source:
- U.S. Census Bureau Gazetteer ZCTA centroids
- https://www2.census.gov/geo/docs/maps-data/data/gazetteer/2025_Gazetteer/2025_Gaz_zcta_national.zip

Generation:
```sh
unzip -p /tmp/2025_Gaz_zcta_national.zip 2025_Gaz_zcta_national.txt \
  | awk -F '|' 'BEGIN{print "zip,lat,lon"} NR>1 && $1 != "" {printf "%s,%s,%s\n", $1, $7, $8}' \
  > internal/locations/zip_centroids.csv
```

`zip_centroids_backfill.csv` fills ZIPs that are missing from the Census ZCTA file.

Source:
- GitHub dataset: `millbj92/US-Zip-Codes-JSON`
- https://raw.githubusercontent.com/millbj92/US-Zip-Codes-JSON/refs/heads/master/USCities.json

Generation:
```sh
jq -r '.[] | [(.zip_code|tonumber), .latitude, .longitude] | @csv' internal/locations/USCities.json \
  | tr -d '"' \
  | awk -F, '{printf "%05d,%s,%s\n", $1, $2, $3}' \
  | sort -u > /tmp/json_centroids.csv

awk -F, 'NR>1 {print $1}' internal/locations/zip_centroids.csv | sort -u > /tmp/census_zips.txt

awk -F, 'BEGIN {print "zip,lat,lon"} NR==FNR {seen[$1]=1; next} !($1 in seen) {print $0}' \
  /tmp/census_zips.txt /tmp/json_centroids.csv \
  > internal/locations/zip_centroids_backfill.csv
```

Notes:
- The GitHub JSON is only an input used to build the backfill CSV; it is not needed at runtime.
- ZIPs are normalized to 5 digits during backfill generation so leading-zero ZIPs are preserved.
