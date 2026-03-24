package wholefoods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"careme/internal/cache"
	"careme/internal/locations/storeindex"
	"careme/internal/sitemapfetch"

	locationtypes "careme/internal/locations/types"
)

const (
	Container = "wholefoods"
	// prefixes are a little redundant since we already have a container. Could simpify with reimport.
	StoreCachePrefix       = "wholefoods/stores/"
	StoreURLMapCacheKey    = "wholefoods/store_url_map.json"
	LocationIDPrefix       = "wholefoods_"
	DefaultStoreSitemapURL = "https://www.wholefoodsmarket.com/sitemap/sitemap-stores.xml"
	LocationIndexCacheKey  = "wholefoods/store_locations.json"
)

type StoreReference struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func SaveStoreURLMap(ctx context.Context, c cache.Cache, refs []StoreReference) error {
	urlMap := make(map[string]string, len(refs))
	for _, ref := range refs {
		if ref.URL == "" || ref.ID == "" {
			continue
		}
		urlMap[ref.URL] = ref.ID
	}

	return sitemapfetch.SaveURLMap(ctx, c, StoreURLMapCacheKey, urlMap)
}

func LoadStoreURLMap(ctx context.Context, c cache.Cache) (map[string]string, error) {
	return sitemapfetch.LoadURLMap(ctx, c, StoreURLMapCacheKey)
}

func CacheStoreSummary(ctx context.Context, c cache.Cache, summary *StoreSummaryResponse) error {
	if summary == nil {
		return errors.New("store summary is required")
	}

	raw, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal store summary: %w", err)
	}

	key := StoreCachePrefix + strconv.Itoa(summary.StoreID)
	if err := c.Put(ctx, key, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write store summary cache: %w", err)
	}
	return nil
}

func loadLocationIndex(ctx context.Context, c cache.Cache) ([]storeindex.Entry, error) {
	entries, err := storeindex.Load(ctx, c, LocationIndexCacheKey)
	if err != nil {
		return nil, fmt.Errorf("load wholefoods locations index: %w", err)
	}
	return entries, nil
}

func RebuildLocationIndex(ctx context.Context, c cache.ListCache, zipLookup storeindex.ZipCentroidLookup) error {
	_, err := storeindex.RebuildFromStoreSummaries(ctx, c, StoreCachePrefix, LocationIndexCacheKey, func(summary StoreSummaryResponse) storeindex.Entry {
		return storeSummaryToIndexEntry(summary, zipLookup)
	})
	return err
}

func loadCachedStoreSummaryByID(ctx context.Context, c cache.Cache, locationID string) (*StoreSummaryResponse, error) {
	normalized, ok := parseLocationID(locationID)
	if !ok {
		return nil, fmt.Errorf("whole foods location %q not found", locationID)
	}
	storeID := strings.TrimPrefix(normalized, LocationIDPrefix)

	reader, err := c.Get(ctx, StoreCachePrefix+storeID)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummaryResponse
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return nil, fmt.Errorf("decode whole foods store summary: %w", err)
	}
	return &summary, nil
}

func storeSummaryToIndexEntry(summary StoreSummaryResponse, zipLookup storeindex.ZipCentroidLookup) storeindex.Entry {
	lat, lon := storeindex.Coordinates(&summary.PrimaryLocation.Latitude, &summary.PrimaryLocation.Longitude, summary.PrimaryLocation.Address.ZipCode, zipLookup)
	return storeindex.Entry{
		ID:  LocationIDPrefix + strconv.Itoa(summary.StoreID),
		Lat: lat,
		Lon: lon,
	}
}

// StoreSummaryToLocation converts a whole food type intoa  generic locaitn.
// Mostly vanilla except for prefixing name
func storeSummaryToLocation(summary StoreSummaryResponse) locationtypes.Location {
	lat := summary.PrimaryLocation.Latitude
	lon := summary.PrimaryLocation.Longitude

	return locationtypes.Location{
		ID:      LocationIDPrefix + strconv.Itoa(summary.StoreID),
		Name:    "Whole Foods " + summary.DisplayName,
		Address: summary.PrimaryLocation.Address.StreetAddressLine1,
		State:   summary.PrimaryLocation.Address.State,
		ZipCode: summary.PrimaryLocation.Address.ZipCode,
		Lat:     &lat,
		Lon:     &lon,
		Chain:   "wholefoods",
	}
}
