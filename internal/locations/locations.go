package locations

import (
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	locationtypes "careme/internal/locations/types"
	"careme/internal/seasons"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
	"careme/internal/walmart"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"
)

type userLookup interface {
	FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error)
}

type locationStorage struct {
	client       []locationBackend
	zipCentroids map[string]ZipCentroid
	cache        cache.Cache
}

// bad for rural areas if zip code is huge?
const maxLocationDistanceMiles = 20.0
const locationCachePrefix = "location/"

type locationServer struct {
	storage     locationGetter
	userStorage userLookup
}

type locationGetter interface {
	GetLocationByID(ctx context.Context, locationID string) (*Location, error)
	GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error)
}

type locationBackend interface {
	locationGetter
	IsID(locationID string) bool
}

// Location is kept as an alias for compatibility with existing imports.
type Location = locationtypes.Location

func New(cfg *config.Config, c cache.Cache) (locationGetter, error) {
	if c == nil {
		return nil, fmt.Errorf("cache is required")
	}
	if cfg.Mocks.Enable {
		return mock{}, nil
	}

	//pass these in?
	var backends []locationBackend
	kclient, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kroger client: %w", err)
	}
	backends = append(backends, kclient)

	if cfg.Walmart.IsEnabled() {
		wclient, err := walmart.NewClient(cfg.Walmart)
		if err != nil {
			return nil, fmt.Errorf("failed to create Walmart client: %w", err)
		}
		backends = append(backends, wclient)
	}
	zipCentroids, err := loadEmbeddedZipCentroids()
	if err != nil {
		return nil, fmt.Errorf("failed to load zip centroids: %w", err)
	}
	return &locationStorage{
		client:       backends,
		zipCentroids: zipCentroids,
		cache:        c,
	}, nil

}

func NewServer(storage locationGetter, userStorage userLookup) *locationServer {
	return &locationServer{
		storage:     storage,
		userStorage: userStorage,
	}
}

func (l *locationStorage) GetLocationByID(ctx context.Context, locationID string) (*Location, error) {
	if cachedLoc, ok := l.cachedLocationByID(ctx, locationID); ok {
		return &cachedLoc, nil
	}

	for _, backend := range l.client {
		if !backend.IsID(locationID) {
			continue
		}

		loc, err := backend.GetLocationByID(ctx, locationID)
		if err != nil {
			return nil, err
		}

		l.storeLocationIfMissing(ctx, *loc)
		return loc, nil
	}
	return nil, fmt.Errorf("location ID %s not supported by any backend", locationID)
}

func (l *locationStorage) GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error) {
	requestedCentroid, hasRequestedCentroid := zipCentroidByZIP(zipcode, l.zipCentroids)
	if !hasRequestedCentroid {
		slog.ErrorContext(ctx, "requested zip has no centroid; skipping distance filter and sort", "zip", zipcode)
		return nil, fmt.Errorf("invalid zip code %s. Can't find lat long", zipcode)
	}

	results := make(chan []Location, len(l.client))
	errors := make(chan error, len(l.client))
	var wg sync.WaitGroup
	for _, backend := range l.client {
		wg.Add(1)
		go func(backend locationBackend) {
			defer wg.Done()
			locations, err := backend.GetLocationsByZip(ctx, zipcode)
			if err != nil {
				slog.ErrorContext(ctx, "error fetching locations from backend", "error", err, "backend", fmt.Sprintf("%T", backend), "zip", zipcode)
				errors <- err
				return
			}
			results <- locations
		}(backend)
	}
	wg.Wait()
	close(results)
	close(errors)
	if len(errors) == len(l.client) {
		return nil, fmt.Errorf("all backends failed to get locations for zip %s", zipcode)
	}
	var allLocations []Location
	for result := range results {
		allLocations = append(allLocations, result...)
	}

	filtered := make([]Location, 0, len(allLocations))
	for _, loc := range allLocations {
		if _, hasZipCentroid := zipCentroidByZIP(loc.ZipCode, l.zipCentroids); !hasZipCentroid {
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

	for _, loc := range allLocations {
		go func(loc Location) {
			//itentionally giving its own
			storeCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			if err := l.storeLocationIfMissing(storeCtx, loc); err != nil {
				slog.WarnContext(ctx, "failed to store location in cache", "location_id", loc.ID, "error", err)
			}
		}(loc)
	}
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

	raw, err := io.ReadAll(blob)
	if err != nil {
		slog.WarnContext(ctx, "failed to read cached location blob", "location_id", locationID, "error", err)
		return Location{}, false
	}
	var loc Location
	if err := json.Unmarshal(raw, &loc); err != nil {
		slog.WarnContext(ctx, "failed to parse cached location blob", "location_id", locationID, "error", err)
		return Location{}, false
	}
	return loc, true
}

func (l *locationStorage) storeLocationIfMissing(ctx context.Context, loc Location) error {
	loc.CachedAt = time.Now().UTC()
	id := locationCachePrefix + loc.ID
	found, err := l.cache.Exists(ctx, id)
	if err != nil {
		slog.WarnContext(ctx, "failed to check if location exists in cache", "location_id", loc.ID, "error", err)
		return err
	}
	if found {
		return nil
	}

	locationJSON, err := json.Marshal(loc)
	if err != nil {
		slog.WarnContext(ctx, "failed to marshal location for cache", "location_id", loc.ID, "error", err)
		return err
	}
	//TODO clean out old ones?
	if err := l.cache.Put(ctx, id, string(locationJSON), cache.IfNoneMatch()); err != nil && !errors.Is(err, cache.ErrAlreadyExists) {
		return err
	}
	return nil
}

func sortLocationsByDistanceFromCentroid(locations []Location, requestedCentroid ZipCentroid, zipCentroids map[string]ZipCentroid) {
	sort.SliceStable(locations, func(i, j int) bool {
		leftDistance := locationDistanceTo(requestedCentroid, locations[i], zipCentroids)
		rightDistance := locationDistanceTo(requestedCentroid, locations[j], zipCentroids)
		return leftDistance < rightDistance
	})
}

func locationDistanceTo(target ZipCentroid, loc Location, zipCentroids map[string]ZipCentroid) float64 {
	lat, lon := locationCoordinates(loc, zipCentroids)
	return haversineMiles(target.Lat, target.Lon, lat, lon)
}

func locationCoordinates(loc Location, zipCentroids map[string]ZipCentroid) (float64, float64) {
	if loc.Lat != nil && loc.Lon != nil {
		return *loc.Lat, *loc.Lon
	}

	//do we actualyl want to fall back?
	centroid, _ := zipCentroidByZIP(loc.ZipCode, zipCentroids)
	return centroid.Lat, centroid.Lon
}

// haversineMiles returns great-circle distance between two latitude/longitude
// points in statute miles. Inputs are decimal degrees.
func haversineMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMiles = 3958.7613
	toRadians := math.Pi / 180.0

	// Convert deltas and absolute latitudes to radians for trig functions.
	dLat := (lat2 - lat1) * toRadians
	dLon := (lon2 - lon1) * toRadians
	lat1Rad := lat1 * toRadians
	lat2Rad := lat2 * toRadians

	// Standard Haversine formula:
	// a = sin²(Δφ/2) + cos φ1 * cos φ2 * sin²(Δλ/2)
	// c = 2 * atan2(√a, √(1-a))
	// d = R * c
	sinHalfDLat := math.Sin(dLat / 2.0)
	sinHalfDLon := math.Sin(dLon / 2.0)
	a := sinHalfDLat*sinHalfDLat + math.Cos(lat1Rad)*math.Cos(lat2Rad)*sinHalfDLon*sinHalfDLon
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))
	return earthRadiusMiles * c
}

func (l *locationServer) Ready(ctx context.Context) error {
	_, err := l.storage.GetLocationsByZip(ctx, "98005") //magic number is my zip code :)
	return err
}

func (l *locationServer) Register(mux *http.ServeMux, authClient auth.AuthClient) {
	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		currentUser, err := l.userStorage.FromRequest(ctx, r, authClient)
		if err != nil {
			if !errors.Is(err, auth.ErrNoSession) {
				http.Error(w, "unable to load account", http.StatusInternalServerError)
				slog.ErrorContext(ctx, "failed to get user from request", "error", err)
				return
			}
		}

		zip := r.URL.Query().Get("zip")
		if zip == "" {
			slog.InfoContext(ctx, "no zip code provided to /locations")
			http.Error(w, "provide a zip code with ?zip=12345", http.StatusBadRequest)
			return
		}
		var favoriteStore string
		if currentUser != nil {
			favoriteStore = currentUser.FavoriteStore
		}
		if err := l.renderLocationsPage(w, ctx, zip, favoriteStore, currentUser != nil); err != nil {
			slog.ErrorContext(ctx, "failed to render locations page", "zip", zip, "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}

func (l *locationServer) renderLocationsPage(w http.ResponseWriter, ctx context.Context, zip string, favoriteStore string, serverSignedIn bool) error {
	locs, err := l.storage.GetLocationsByZip(ctx, zip)
	if err != nil {
		return fmt.Errorf("failed to get locations for zip %s: %w", zip, err)
	}

	data := struct {
		Locations      []Location
		Zip            string
		FavoriteStore  string
		ClarityScript  template.HTML
		Style          seasons.Style
		ServerSignedIn bool
	}{
		Locations:      locs,
		Zip:            zip,
		FavoriteStore:  favoriteStore,
		ClarityScript:  templates.ClarityScript(),
		Style:          seasons.GetCurrentStyle(),
		ServerSignedIn: serverSignedIn,
	}
	return templates.Location.Execute(w, data)
}
