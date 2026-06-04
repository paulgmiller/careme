package heb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"careme/internal/albertsons"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations/hydrator"

	locationtypes "careme/internal/locations/types"
)

const defaultStoreLocationSearchMaxPages = 20

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type storeLocationsFetcher interface {
	StoreLocationsPage(ctx context.Context, buildID, address string, page int) ([]byte, error)
}

type LocationBackend struct {
	cache          cache.Cache
	client         storeLocationsFetcher
	loadBuildID    func(context.Context) (string, error)
	hydrator       *hydrator.LazyHydrator
	maxSearchPages int
}

func NewLocationBackendFromConfig(ctx context.Context, cfg *config.Config, zipLookup centroidByZip) (*LocationBackend, error) {
	if !cfg.HEB.IsEnabled() {
		return nil, locationtypes.DisabledBackendError("HEB")
	}

	if zipLookup == nil {
		return nil, fmt.Errorf("zip centroid lookup is required")
	}

	listCache, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create HEB list cache: %w", err)
	}

	albertsonsCache, err := cache.EnsureCache(albertsons.Container)
	if err != nil {
		return nil, fmt.Errorf("create albertsons cache for HEB store locations reese84 token: %w", err)
	}

	return newLocationBackendWithDeps(ctx, listCache, zipLookup, NewStoreLocationsClient(StoreLocationsClientConfig{
		Reese84Provider: cachedAlbertsonsReese84Provider(albertsonsCache),
	}), CachedNextDataBuildIDProvider(listCache, ""))
}

func newLocationBackendWithDeps(
	_ context.Context,
	c cache.Cache,
	zipLookup centroidByZip,
	client storeLocationsFetcher,
	loadBuildID func(context.Context) (string, error),
) (*LocationBackend, error) {
	if c == nil {
		return nil, fmt.Errorf("cache is required")
	}
	if zipLookup == nil {
		return nil, fmt.Errorf("zip centroid lookup is required")
	}
	if client == nil {
		return nil, fmt.Errorf("store locations client is required")
	}
	if loadBuildID == nil {
		return nil, fmt.Errorf("heb next data build id provider is required")
	}

	return &LocationBackend{
		cache:          c,
		client:         client,
		loadBuildID:    loadBuildID,
		hydrator:       hydrator.NewLazyHydrator(&loader{c}),
		maxSearchPages: defaultStoreLocationSearchMaxPages,
	}, nil
}

func cachedAlbertsonsReese84Provider(c cache.Cache) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		record, err := albertsons.LoadLatestReese84(ctx, c)
		if err != nil {
			return "", err
		}
		return record.Cookie, nil
	}
}

func (b *LocationBackend) IsID(locationID string) bool {
	return IsID(locationID)
}

func (*LocationBackend) HasInventory(locationID string) bool {
	return IsID(locationID)
}

func (b *LocationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	locationID = strings.TrimSpace(locationID)
	if !IsID(locationID) {
		return nil, fmt.Errorf("heb location id %q is invalid", locationID)
	}

	loc, err := b.hydrator.Hydrate(ctx, locationID)
	if err != nil {
		return nil, err
	}
	copy := loc
	return &copy, nil
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	zipcode = strings.TrimSpace(zipcode)
	if zipcode == "" {
		return nil, fmt.Errorf("zipcode is required")
	}

	summaries, err := b.fetchStoreSummariesByAddress(ctx, zipcode)
	if err != nil {
		return nil, err
	}

	locations := make([]locationtypes.Location, 0, len(summaries))
	for _, summary := range summaries {
		summary := summary
		if err := CacheStoreSummary(ctx, b.cache, &summary); err != nil {
			return nil, err
		}
		loc, err := b.hydrator.Hydrate(ctx, summary.ID)
		if err != nil {
			return nil, err
		}
		locations = append(locations, loc)
	}
	return locations, nil
}

func (b *LocationBackend) fetchStoreSummariesByAddress(ctx context.Context, address string) ([]StoreSummary, error) {
	seen := make(map[string]struct{})
	summaries := make([]StoreSummary, 0)
	fetchedForAddress := 0

	maxPages := b.maxSearchPages
	if maxPages <= 0 {
		maxPages = defaultStoreLocationSearchMaxPages
	}

	for pageNumber := 1; pageNumber <= maxPages; pageNumber++ {
		page, err := b.loadStoreLocationsPage(ctx, address, pageNumber)
		if err != nil {
			return nil, err
		}
		if len(page.Summaries) == 0 {
			break
		}

		for _, summary := range page.Summaries {
			if _, ok := seen[summary.ID]; ok {
				continue
			}
			seen[summary.ID] = struct{}{}
			summaries = append(summaries, summary)
		}

		fetchedForAddress += len(page.Summaries)
		if page.TotalStoresCount > 0 && fetchedForAddress >= page.TotalStoresCount {
			break
		}
	}
	return summaries, nil
}

func (b *LocationBackend) loadStoreLocationsPage(ctx context.Context, address string, pageNumber int) (*StoreLocationsPage, error) {
	page, err := LoadCachedStoreLocationsPage(ctx, b.cache, address, pageNumber)
	if err == nil {
		return page, nil
	}
	var invalidJSON invalidStoreLocationsJSONError
	if !errors.Is(err, cache.ErrNotFound) && !errors.As(err, &invalidJSON) {
		return nil, err
	}

	buildID, err := b.loadBuildID(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve heb next data build id: %w", err)
	}
	body, err := b.client.StoreLocationsPage(ctx, buildID, address, pageNumber)
	if err != nil {
		freshBuildID, refreshErr := DiscoverNextDataBuildIDFromEnv(ctx)
		if refreshErr != nil {
			return nil, err
		}
		if freshBuildID == buildID {
			return nil, err
		}
		if saveErr := SaveNextDataBuildID(ctx, b.cache, freshBuildID); saveErr != nil {
			return nil, saveErr
		}
		body, err = b.client.StoreLocationsPage(ctx, freshBuildID, address, pageNumber)
		if err != nil {
			return nil, err
		}
	}
	if err := CacheStoreLocationsPage(ctx, b.cache, address, pageNumber, body); err != nil {
		return nil, err
	}
	return DecodeStoreLocationsPage(body)
}
