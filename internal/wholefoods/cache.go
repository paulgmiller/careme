package wholefoods

import (
	"bufio"
	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/samber/lo"
	lop "github.com/samber/lo/parallel"
)

const (
	StoreCachePrefix       = "wholefoods/stores/"
	StoreURLMapCacheKey    = "wholefoods/store_url_map.json"
	LocationIDPrefix       = "wholefoods_"
	DefaultStoreSitemapURL = "https://www.wholefoodsmarket.com/sitemap/sitemap-stores.xml"
)

type StoreReference struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func ReadStoreReferences(r io.Reader) ([]StoreReference, error) {
	scanner := bufio.NewScanner(r)
	refs := make([]StoreReference, 0)
	seen := make(map[string]struct{})

	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("parse store reference line %d: expected <id> <url>", lineNo)
		}

		ref := StoreReference{
			ID:  strings.TrimSpace(fields[0]),
			URL: strings.TrimSpace(fields[1]),
		}
		if ref.ID == "" || ref.URL == "" {
			return nil, fmt.Errorf("parse store reference line %d: store id and url are required", lineNo)
		}
		if !isAllDigits(ref.ID) {
			return nil, fmt.Errorf("parse store reference line %d: invalid store id %q", lineNo, ref.ID)
		}
		if _, ok := seen[ref.ID]; ok {
			continue
		}
		seen[ref.ID] = struct{}{}
		refs = append(refs, ref)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan store references: %w", err)
	}
	return refs, nil
}

func SaveStoreURLMap(ctx context.Context, c cache.Cache, refs []StoreReference) error {
	urlMap := make(map[string]string, len(refs))
	for _, ref := range refs {
		if ref.URL == "" || ref.ID == "" {
			continue
		}
		urlMap[ref.URL] = ref.ID
	}

	raw, err := json.Marshal(urlMap)
	if err != nil {
		return fmt.Errorf("marshal store url map: %w", err)
	}
	if err := c.Put(ctx, StoreURLMapCacheKey, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write store url map cache: %w", err)
	}
	return nil
}

func LoadStoreURLMap(ctx context.Context, c cache.Cache) (map[string]string, error) {
	reader, err := c.Get(ctx, StoreURLMapCacheKey)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read store url map cache: %w", err)
	}

	var urlMap map[string]string
	if err := json.Unmarshal(raw, &urlMap); err != nil {
		return nil, fmt.Errorf("decode store url map cache: %w", err)
	}
	return urlMap, nil
}

func StoreReferencesFromURLMap(urlMap map[string]string) []StoreReference {
	refs := make([]StoreReference, 0, len(urlMap))
	for url, id := range urlMap {
		if url == "" || id == "" {
			continue
		}
		refs = append(refs, StoreReference{ID: id, URL: url})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ID == refs[j].ID {
			return refs[i].URL < refs[j].URL
		}
		return refs[i].ID < refs[j].ID
	})
	return refs
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

func LoadCachedStoreSummaries(ctx context.Context, c cache.ListCache) ([]*StoreSummaryResponse, error) {
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

		raw, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			slog.WarnContext(ctx, "failed to read cached whole foods store summary bytes", "key", key, "error", readErr)
			return nil
		}

		var summary StoreSummaryResponse
		if err := json.Unmarshal(raw, &summary); err != nil {
			slog.WarnContext(ctx, "failed to decode cached whole foods store summary", "key", key, "error", err)
			return nil
		}
		return &summary
	})

	summaries = lo.Compact(summaries)

	if len(summaries) == 0 {
		return nil, fmt.Errorf("failed to load locations")
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].StoreID < summaries[j].StoreID
	})
	return summaries, nil
}

func StoreSummaryToLocation(summary StoreSummaryResponse) locationtypes.Location {
	lat := summary.PrimaryLocation.Latitude
	lon := summary.PrimaryLocation.Longitude

	return locationtypes.Location{
		ID:      LocationIDPrefix + strconv.Itoa(summary.StoreID),
		Name:    summary.DisplayName,
		Address: summary.PrimaryLocation.Address.StreetAddressLine1,
		State:   summary.PrimaryLocation.Address.State,
		ZipCode: summary.PrimaryLocation.Address.ZipCode,
		Lat:     &lat,
		Lon:     &lon,
	}
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
