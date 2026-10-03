package locations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"careme/internal/cache"
	"careme/internal/locations/geo"
	"careme/internal/locations/nearby"
	"careme/internal/logsetup"
	"careme/internal/parallelism"

	locationtypes "careme/internal/locations/types"

	"github.com/samber/lo"
)

type locationStorage struct {
	clients      []locationBackend
	zipCentroids centroidByZip
	cache        cache.ListCache
	signature    func(string) string
}

type locationGetter interface {
	GetLocationByID(ctx context.Context, locationID string) (*Location, error)
	GetLocationsByCoordinates(ctx context.Context, coordinates geo.Coordinate) ([]Location, error)
	HasInventory(locationID string) bool
}

type LocationBackend interface {
	locationGetter
	IsID(locationID string) bool
}
type locationBackend = LocationBackend

// locationCachePolicy is optional; backends without it are cacheable.
type locationCachePolicy interface {
	IsCacheable() bool
}

// name is terrible conflicting with locationStorage. locationStorage should become locationAggregator.
type Store interface {
	locationGetter
	RequestStore(ctx context.Context, locationID string) error
	RequestedStoreIDs(ctx context.Context) ([]string, error)
}
type locationStore = Store

// Location is kept as an alias for compatibility with existing imports.
type Location = locationtypes.Location

type CentroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}
type centroidByZip = CentroidByZip

type (
	LocationBackendFactory func(context.Context) (LocationBackend, error)
	locationBackendFactory = LocationBackendFactory
)

const (
	locationCachePrefix = "location/"
	storeRequestPrefix  = "location-store-requests/"
)

func New(c cache.ListCache, centroids CentroidByZip, factories []LocationBackendFactory, signature func(string) string) (Store, error) {
	if c == nil {
		return nil, fmt.Errorf("cache is required")
	}
	backends, err := initializeLocationBackends(context.Background(), factories)
	if err != nil {
		return nil, err
	}

	return &locationStorage{
		clients:      backends,
		zipCentroids: centroids,
		cache:        c,
		signature:    signature,
	}, nil
}

func initializeLocationBackends(ctx context.Context, factories []locationBackendFactory) ([]locationBackend, error) {
	results, err := parallelism.MapWithErrors(factories, func(factory locationBackendFactory) (locationBackend, error) {
		start := time.Now()
		backend, err := factory(ctx)
		if err != nil {
			if locationtypes.IsDisabledBackendError(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("failed to initialize location backend %t: %w", backend, err)
		}
		slog.InfoContext(ctx, "initialized location backend", "backend", fmt.Sprintf("%T", backend), "latencyMS", time.Since(start).Milliseconds())
		return backend, nil
	})
	if err != nil {
		return nil, err
	}
	return lo.Compact(results), nil
}

func (l *locationStorage) HasInventory(locationID string) bool {
	_, found := lo.Find(l.clients, func(backend locationBackend) bool {
		return backend.IsID(locationID) && backend.HasInventory(locationID)
	})
	return found
}

// Backends without an explicit cache policy retain the default of caching locations.
func cachable(backend locationBackend) bool {
	if policy, ok := backend.(locationCachePolicy); ok {
		return policy.IsCacheable()
	}
	return true
}

func (l *locationStorage) GetLocationByID(ctx context.Context, locationID string) (*Location, error) {
	for _, backend := range l.clients {
		if !backend.IsID(locationID) {
			continue
		}
		cachable := cachable(backend)
		if cachable {
			if cachedLoc, ok := l.cachedLocationByID(ctx, locationID); ok {
				// could relook up on error here.
				loc, err := backfillLocationCoordinates(cachedLoc, l.zipCentroids)
				if err != nil {
					return nil, err
				}
				l.setStaplesSignature(loc)
				return loc, nil
			}
		}

		loc, err := backend.GetLocationByID(ctx, locationID)
		if err != nil {
			return nil, err
		}
		l.setStaplesSignature(loc)
		loc, err = backfillLocationCoordinates(*loc, l.zipCentroids)
		if err != nil {
			return nil, err
		}

		if cachable {
			go func() {
				if err := l.storeLocationIfMissing(*loc); err != nil {
					slog.WarnContext(ctx, "failed to store location in cache", "location_id", loc.ID, "error", err)
				}
			}()
		}
		return loc, nil
	}
	return nil, fmt.Errorf("location ID %s not supported by any backend", locationID)
}

func (l *locationStorage) GetLocationsByCoordinates(ctx context.Context, coordinates geo.Coordinate) ([]Location, error) {
	if err := coordinates.Valid(); err != nil {
		return nil, err
	}

	allLocations, fetcherrors := parallelism.Flatten(l.clients, func(backend locationBackend) ([]*Location, error) {
		start := time.Now()
		locations, err := backend.GetLocationsByCoordinates(ctx, coordinates)
		if err != nil {
			level := slog.LevelError
			if errors.Is(ctx.Err(), context.Canceled) && errors.Is(err, context.Canceled) {
				level = slog.LevelDebug
			}
			slog.Log(ctx, level, "error fetching locations from backend", "error", err, "backend", fmt.Sprintf("%T", backend), "lat", coordinates.Lat, "lon", coordinates.Lon)
			return nil, err
		}
		slog.InfoContext(ctx, "Got results for backend", "backend", fmt.Sprintf("%T", backend), "lat", coordinates.Lat, "lon", coordinates.Lon, "count", len(locations), "latencyMS", time.Since(start).Milliseconds())
		locations = lo.Take(locations, nearby.MaxLocationCount)
		hydrated := make([]*Location, 0, len(locations))
		for _, loc := range locations {
			backfilled, err := backfillLocationCoordinates(loc, l.zipCentroids)
			if err != nil {
				slog.WarnContext(ctx, "location has no coordinates; skipping result", "location_id", loc.ID, "zip", loc.ZipCode, "error", err)
				continue
			}
			l.setStaplesSignature(backfilled)
			hydrated = append(hydrated, backfilled)
		}
		if cachable(backend) {
			for _, loc := range hydrated {
				go func() {
					if err := l.storeLocationIfMissing(*loc); err != nil {
						slog.WarnContext(ctx, "failed to store location in cache", "location_id", loc.ID, "error", err)
					}
				}()
			}
		}
		return hydrated, nil
	})

	// Cancellation applies to the whole search, even if some backends succeeded.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	filtered := make([]Location, 0, len(allLocations))
	for _, loc := range allLocations {
		distance := geo.HaversineMiles(coordinates, loc.Coordinate())
		if distance > nearby.MaxLocationDistanceMiles {
			slog.DebugContext(ctx, "dropping location beyond max distance", "location_id", loc.ID, "zip", loc.ZipCode, "distance_miles", distance, "max_distance_miles", nearby.MaxLocationDistanceMiles)
			continue
		}
		filtered = append(filtered, *loc)
	}

	if len(filtered) == 0 {
		return nil, fetcherrors
	}

	sortLocationsByDistanceFromCoordinates(filtered, coordinates)
	// as long a we got some results try and show them
	// could also desploy to user the chains we failed to query
	return filtered, nil
}

func (l *locationStorage) setStaplesSignature(loc *Location) {
	if l.signature != nil {
		loc.StaplesSignature = l.signature(loc.ID)
	}
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

func sortLocationsByDistanceFromCoordinates(locations []Location, coordinates geo.Coordinate) {
	sort.SliceStable(locations, func(i, j int) bool {
		leftDistance := geo.HaversineMiles(coordinates, locations[i].Coordinate())
		rightDistance := geo.HaversineMiles(coordinates, locations[j].Coordinate())
		return leftDistance < rightDistance
	})
}

func backfillLocationCoordinates(loc Location, zipCentroids centroidByZip) (*Location, error) {
	if loc.Lat != nil && loc.Lon != nil {
		return &loc, nil
	}
	centroid, ok := zipCentroids.ZipCentroidByZIP(loc.ZipCode)
	if !ok {
		return nil, fmt.Errorf("no lat/long and bad zip %s", loc.ZipCode)
	}
	loc.Lat = &centroid.Lat
	loc.Lon = &centroid.Lon
	return &loc, nil
}
