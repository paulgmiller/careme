package wholefoods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	"careme/internal/sitemapfetch"

	"github.com/samber/lo"
	lop "github.com/samber/lo/parallel"
)

const (
	Container = "wholefoods"
	// prefixes are a little redundant since we already have a container. Could simpify with reimport.
	StoreCachePrefix       = "wholefoods/stores/"
	StoreURLMapCacheKey    = "wholefoods/store_url_map.json"
	LocationIDPrefix       = "wholefoods_"
	DefaultStoreSitemapURL = "https://www.wholefoodsmarket.com/sitemap/sitemap-stores.xml"
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

// loadCachedStoreSummaries get all store summaries into memory.
// its pretty intense and maybe we should just load latlong for index
func loadCachedStoreSummaries(ctx context.Context, c cache.ListCache) ([]*StoreSummaryResponse, error) {
	keys, err := c.List(ctx, StoreCachePrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list cached store summaries: %w", err)
	}

	summaries := lop.Map(keys, func(key string, i int) *StoreSummaryResponse {
		reader, err := c.Get(ctx, StoreCachePrefix+key)
		if err != nil {
			slog.WarnContext(ctx, "failed to read cached whole foods store summary", "key", key, "error", err)
			return nil
		}
		defer func() {
			_ = reader.Close()
		}()
		var summary StoreSummaryResponse
		if err := json.NewDecoder(reader).Decode(&summary); err != nil {
			slog.WarnContext(ctx, "failed to decode cached whole foods store summary", "key", key, "error", err)
			return nil
		}
		return &summary
	})

	summaries = lo.Compact(summaries)

	if len(summaries) == 0 {
		return nil, fmt.Errorf("failed to load wholefoods locations")
	}

	return summaries, nil
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
