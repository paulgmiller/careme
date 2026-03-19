package locations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"careme/internal/albertsons"
	"careme/internal/aldi"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/heb"
	"careme/internal/kroger"
	"careme/internal/locations/geo"
	"careme/internal/logsetup"
	"careme/internal/publix"
	"careme/internal/walmart"
	"careme/internal/wholefoods"

	locationtypes "careme/internal/locations/types"

	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"
)

type locationStorage struct {
	clients      []locationBackend
	zipCentroids centroidByZip
	cache        cache.ListCache
}

type locationGetter interface {
	GetLocationByID(ctx context.Context, locationID string) (*Location, error)
	GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error)
	HasInventory(locationID string) bool
}

type locationBackend interface {
	locationGetter
	IsID(locationID string) bool
}

// name is terrible conflicting with locationStorage. locationStorage should become locationAggregator.
type locationStore interface {
	locationGetter
	RequestStore(ctx context.Context, locationID string) error
	RequestedStoreIDs(ctx context.Context) ([]string, error)
}

// Location is kept as an alias for compatibility with existing imports.
type Location = locationtypes.Location

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type locationBackendFactory func(context.Context) (locationBackend, error)

// bad for rural areas if zip code is huge?
const (
	maxLocationDistanceMiles = 20.0
	locationCachePrefix      = "location/"
	storeRequestPrefix       = "location-store-requests/"
)

func New(cfg *config.Config, c cache.ListCache, centroids centroidByZip) (locationStore, error) {
	if c == nil {
		return nil, fmt.Errorf("cache is required")
	}
	if cfg.Mocks.Enable {
		// should probably have something else return th mock so we can just return concerete type here.
		return mock{}, nil
	}

	ctx := context.Background()
	backendfactories := []locationBackendFactory{
		func(context.Context) (locationBackend, error) { return kroger.FromConfig(cfg) },
		func(context.Context) (locationBackend, error) { return walmart.NewClient(cfg.Walmart) },
		func(ctx context.Context) (locationBackend, error) {
			return aldi.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
		func(ctx context.Context) (locationBackend, error) {
			return wholefoods.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
		func(ctx context.Context) (locationBackend, error) {
			return albertsons.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
		func(ctx context.Context) (locationBackend, error) {
			return publix.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
		func(ctx context.Context) (locationBackend, error) {
			return heb.NewLocationBackendFromConfig(ctx, cfg, centroids)
		},
	}

	backends, err := initializeLocationBackends(ctx, backendfactories)
	if err != nil {
		return nil, err
	}

	return &locationStorage{
		clients:      backends,
		zipCentroids: centroids,
		cache:        c,
	}, nil
}

func initializeLocationBackends(ctx context.Context, factories []locationBackendFactory) ([]locationBackend, error) {
	g, ctx := errgroup.WithContext(ctx)
	results := make(chan locationBackend, len(factories))
	for i, factory := range factories {
		i, factory := i, factory
		g.Go(func() error {
			start := time.Now()
			backend, err := factory(ctx)
			if err != nil {
				if locationtypes.IsDisabledBackendError(err) {
					return nil
				}
				return fmt.Errorf("failed to initialize location backend %d: %w", i, err)
			}
			slog.InfoContext(ctx, "initialized location backend", "backend", fmt.Sprintf("%T", backend), "latencyMS", time.Since(start).Milliseconds())
			results <- backend
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	close(results)

	return lo.ChannelToSlice(results), nil
}

func (l *locationStorage) HasInventory(locationID string) bool {
	_, found := lo.Find(l.clients, func(backend locationBackend) bool {
		return backend.IsID(locationID) && backend.HasInventory(locationID)
	})
	return found
}

func (l *locationStorage) GetLocationByID(ctx context.Context, locationID string) (*Location, error) {
	if cachedLoc, ok := l.cachedLocationByID(ctx, locationID); ok {
		return &cachedLoc, nil
	}

	for _, backend := range l.clients {
		if !backend.IsID(locationID) {
			continue
		}

		loc, err := backend.GetLocationByID(ctx, locationID)
		if err != nil {
			return nil, err
		}

		go func() {
			if err := l.storeLocationIfMissing(*loc); err != nil {
				slog.WarnContext(ctx, "failed to store location in cache", "location_id", loc.ID, "error", err)
			}
		}()
		return loc, nil
	}
	return nil, fmt.Errorf("location ID %s not supported by any backend", locationID)
}

func (l *locationStorage) GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error) {
	results := make(chan []Location, len(l.clients))
	errors := make(chan error, len(l.clients))
	var wg sync.WaitGroup
	for _, backend := range l.clients {
		wg.Add(1)
		go func(backend locationBackend) {
			defer wg.Done()
			start := time.Now()
			locations, err := backend.GetLocationsByZip(ctx, zipcode)
			if err != nil {
				slog.ErrorContext(ctx, "error fetching locations from backend", "error", err, "backend", fmt.Sprintf("%T", backend), "zip", zipcode)
				errors <- err
				return
			}
			slog.InfoContext(ctx, "Got results for backend", "backend", fmt.Sprintf("%T", backend), "zip", zipcode, "count", len(locations), "latencyMS", time.Since(start).Milliseconds())
			results <- locations
		}(backend)
	}
	wg.Wait()
	close(results)
	close(errors)
	if len(errors) == len(l.clients) {
		return nil, fmt.Errorf("all backends failed to get locations for zip %s", zipcode)
	}
	var allLocations []Location
	for result := range results {
		allLocations = append(allLocations, result...)
	}

	for _, loc := range allLocations {
		go func(loc Location) {
			if err := l.storeLocationIfMissing(loc); err != nil {
				slog.WarnContext(ctx, "failed to store location in cache", "location_id", loc.ID, "error", err)
			}
		}(loc)
	}

	requestedCentroid, hasRequestedCentroid := l.zipCentroids.ZipCentroidByZIP(zipcode)
	if !hasRequestedCentroid {
		// were missign zip codes. Make this an error later?
		slog.WarnContext(ctx, "requested zip has no centroid; returning unsorted locations without distance filter", "zip", zipcode, "count", len(allLocations))
		return allLocations, nil
	}

	filtered := make([]Location, 0, len(allLocations))
	for _, loc := range allLocations {
		if _, hasZipCentroid := l.zipCentroids.ZipCentroidByZIP(loc.ZipCode); !hasZipCentroid {
			slog.WarnContext(ctx, "location has no zip centroid; skipping distance filter and sort", "location_id", loc.ID, "zip", loc.ZipCode)
			continue
		}

		distance := locationDistanceTo(requestedCentroid, loc, l.zipCentroids)
		if distance > maxLocationDistanceMiles {
			slog.DebugContext(ctx, "dropping location beyond max distance", "location_id", loc.ID, "zip", loc.ZipCode, "distance_miles", distance, "max_distance_miles", maxLocationDistanceMiles)
			continue
		}
		filtered = append(filtered, loc)
	}
	allLocations = filtered
	sortLocationsByDistanceFromCentroid(allLocations, requestedCentroid, l.zipCentroids)

	return allLocations, nil
}

func (l *locationStorage) cachedLocationByID(ctx context.Context, locationID string) (Location, bool) {
	blob, err := l.cache.Get(ctx, locationCachePrefix+locationID)
	if err != nil {
		return Location{}, false
	}
	defer func() {
		_ = blob.Close()
	}()

	var loc Location
	if err := json.NewDecoder(blob).Decode(&loc); err != nil {
		slog.WarnContext(ctx, "failed to parse cached location blob", "location_id", locationID, "error", err)
		return Location{}, false
	}
	return loc, true
}

func (l *locationStorage) storeLocationIfMissing(loc Location) error {
	// itentionally giving its own context so its not canceled
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	loc.CachedAt = time.Now().UTC()
	id := locationCachePrefix + loc.ID
	found, err := l.cache.Exists(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to check location cache: %w", err)
	}
	if found {
		return nil
	}

	locationJSON, err := json.Marshal(loc)
	if err != nil {
		return fmt.Errorf("failed to marshal location for cache: %w", err)
	}
	// TODO clean out old ones?
	if err := l.cache.Put(ctx, id, string(locationJSON), cache.IfNoneMatch()); err != nil && !errors.Is(err, cache.ErrAlreadyExists) {
		return err
	}
	return nil
}

type locationRequest struct {
	StoreID     string    `json:"store_id"`
	Users       []string  `json:"users"`
	RequestedAt time.Time `json:"requested_at"`
}

func (l *locationStorage) RequestStore(ctx context.Context, storeID string) error {
	request := locationRequest{
		StoreID:     storeID,
		RequestedAt: time.Now().UTC(),
	}
	if current, err := l.cache.Get(ctx, storeRequestPrefix+storeID); err == nil {
		defer func() {
			_ = current.Close()
		}()
		var existingRequest locationRequest
		if err := json.NewDecoder(current).Decode(&existingRequest); err != nil {
			return fmt.Errorf("parse existing store request: %w", err)
		}
		request = existingRequest
	} else if !errors.Is(err, cache.ErrNotFound) {
		return fmt.Errorf("fetch existing store request: %w", err)
	}
	if sessionID, ok := logsetup.SessionIDFromContext(ctx); ok {
		request.Users = append(request.Users, sessionID)
	}

	raw, err := json.Marshal(request)
	if err != nil {
		return nil
	}
	requestKey := storeRequestPrefix + storeID
	if err := l.cache.Put(ctx, requestKey, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("store request put: %w", err)
	}
	return nil
}

func (l *locationStorage) RequestedStoreIDs(ctx context.Context) ([]string, error) {
	storeIDs, err := l.cache.List(ctx, storeRequestPrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list requested stores: %w", err)
	}
	return storeIDs, nil
}

func sortLocationsByDistanceFromCentroid(locations []Location, requestedCentroid locationtypes.ZipCentroid, zipCentroids centroidByZip) {
	sort.SliceStable(locations, func(i, j int) bool {
		leftDistance := locationDistanceTo(requestedCentroid, locations[i], zipCentroids)
		rightDistance := locationDistanceTo(requestedCentroid, locations[j], zipCentroids)
		return leftDistance < rightDistance
	})
}

func locationDistanceTo(target locationtypes.ZipCentroid, loc Location, zipCentroids centroidByZip) float64 {
	lat, lon := locationCoordinates(loc, zipCentroids)
	return geo.HaversineMiles(target.Lat, target.Lon, lat, lon)
}

func locationCoordinates(loc Location, zipCentroids centroidByZip) (float64, float64) {
	if loc.Lat != nil && loc.Lon != nil {
		return *loc.Lat, *loc.Lon
	}

	// do we actualyl want to fall back?
	centroid, _ := zipCentroids.ZipCentroidByZIP(loc.ZipCode)
	return centroid.Lat, centroid.Lon
}
