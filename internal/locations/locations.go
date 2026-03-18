package locations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"careme/internal/albertsons"
	"careme/internal/aldi"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/heb"
	"careme/internal/kroger"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
	"careme/internal/publix"
	"careme/internal/seasons"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
	"careme/internal/walmart"
	"careme/internal/wholefoods"
)

type userLookup interface {
	FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error)
}

type locationStorage struct {
	clients      []locationBackend
	zipCentroids centroidByZip
	cache        cache.Cache
}

// bad for rural areas if zip code is huge?
const (
	maxLocationDistanceMiles = 20.0
	locationCachePrefix      = "location/"
	storeRequestPrefix       = "location-store-requests/"
)

type locationServer struct {
	storage     locationStore
	zipFetcher  zipFetcher
	userStorage userLookup
	cache       cache.Cache
}

type locationGetter interface {
	GetLocationByID(ctx context.Context, locationID string) (*Location, error)
	GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error)
	HasInventory(locationID string) bool
}

type zipFetcher interface {
	NearestZIPToCoordinates(lat, lon float64) (string, bool)
}

type locationBackend interface {
	locationGetter
	IsID(locationID string) bool
}

// name is terrible conflicting with locationStorage. locationStorage should become locationAggregator.
type locationStore interface {
	locationGetter
	RequestStore(ctx context.Context, locationID string) error
}

// Location is kept as an alias for compatibility with existing imports.
type Location = locationtypes.Location

type centroidByZip interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

func New(cfg *config.Config, c cache.Cache, centroids centroidByZip) (locationStore, error) {
	if c == nil {
		return nil, fmt.Errorf("cache is required")
	}
	if cfg.Mocks.Enable {
		// should probably have something else return th mock so we can just return concerete type here.
		return mock{}, nil
	}

	ctx := context.Background()
	type locationBackendFactory func() (locationBackend, error)
	backendfactories := []locationBackendFactory{
		func() (locationBackend, error) { return kroger.FromConfig(cfg) },
		func() (locationBackend, error) { return walmart.NewClient(cfg.Walmart) },
		func() (locationBackend, error) { return aldi.NewLocationBackendFromConfig(ctx, cfg, centroids) },
		func() (locationBackend, error) { return wholefoods.NewLocationBackendFromConfig(ctx, cfg, centroids) },
		func() (locationBackend, error) { return albertsons.NewLocationBackendFromConfig(ctx, cfg, centroids) },
		func() (locationBackend, error) { return publix.NewLocationBackendFromConfig(ctx, cfg, centroids) },
		func() (locationBackend, error) { return heb.NewLocationBackendFromConfig(ctx, cfg, centroids) },
	}

	backends := make([]locationBackend, 0, len(backendfactories))
	for i, factory := range backendfactories {
		backend, err := factory()
		if err != nil {
			if locationtypes.IsDisabledBackendError(err) {
				continue
			}
			return nil, fmt.Errorf("failed to initialize location backend %d:  %w", i, err)
		}
		backends = append(backends, backend)
	}

	return &locationStorage{
		clients:      backends,
		zipCentroids: centroids,
		cache:        c,
	}, nil
}

func NewServer(storage locationStore, zipFetcher zipFetcher, userStorage userLookup, c cache.Cache) *locationServer {
	return &locationServer{
		storage:     storage,
		zipFetcher:  zipFetcher,
		userStorage: userStorage,
		cache:       c,
	}
}

func isHTMXRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

func (l *locationStorage) HasInventory(locationID string) bool {
	for _, backend := range l.clients {
		if backend.IsID(locationID) {
			return backend.HasInventory(locationID)
		}
	}
	return false
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
	raw, err := json.Marshal(request)
	if err != nil {
		return nil
	}
	requestKey := storeRequestPrefix + storeID
	if err := l.cache.Put(ctx, requestKey, string(raw), cache.IfNoneMatch()); err != nil {
		if !errors.Is(err, cache.ErrAlreadyExists) {
			return nil
		}
		return err
	}
	return nil
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

func (l *locationServer) Ready(ctx context.Context) error {
	_, err := l.storage.GetLocationsByZip(ctx, "98005") // magic number is my zip code :)
	return err
}

func (l *locationServer) Register(mux *http.ServeMux, authClient auth.AuthClient) {
	mux.HandleFunc("GET /locations/zip-from-coordinates", func(w http.ResponseWriter, r *http.Request) {
		lat, err := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
		if err != nil {
			http.Error(w, "invalid latitude", http.StatusBadRequest)
			return
		}
		lon, err := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
		if err != nil {
			http.Error(w, "invalid longitude", http.StatusBadRequest)
			return
		}

		zip, ok := l.zipFetcher.NearestZIPToCoordinates(lat, lon)
		if !ok {
			http.Error(w, "zip not found for coordinates", http.StatusNotFound)
			return
		}

		http.Redirect(w, r, "/locations?zip="+url.QueryEscape(zip), http.StatusFound)
	})

	mux.HandleFunc("GET /locations", func(w http.ResponseWriter, r *http.Request) {
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
			http.Error(w, "Failed to render locations page. ", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("POST /locations/request-store", func(w http.ResponseWriter, r *http.Request) {
		if !isHTMXRequest(r) {
			http.Error(w, "store requests must be made via HTMX", http.StatusBadRequest)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		storeID := r.FormValue("store_id")
		if storeID == "" {
			http.Error(w, "store_id is required", http.StatusBadRequest)
			return
		}

		if err := l.storage.RequestStore(r.Context(), storeID); err != nil {
			http.Error(w, "failed to submit request", http.StatusInternalServerError)
			return
		}

		if err := templates.Location.ExecuteTemplate(w, "location_request_store_success", nil); err != nil {
			slog.ErrorContext(r.Context(), "failed to render request-store success fragment", "store_id", storeID, "error", err)
			http.Error(w, "failed to submit request", http.StatusInternalServerError)
			return
		}
	})
}

func (l *locationServer) renderLocationsPage(w http.ResponseWriter, ctx context.Context, zip string, favoriteStore string, serverSignedIn bool) error {
	locs, err := l.storage.GetLocationsByZip(ctx, zip)
	if err != nil {
		return fmt.Errorf("failed to get locations for zip %s: %w", zip, err)
	}

	type locationRow struct {
		Location
		SupportsStaples bool
	}

	rows := make([]locationRow, 0, len(locs))
	for _, loc := range locs {
		rows = append(rows, locationRow{
			Location:        loc,
			SupportsStaples: l.storage.HasInventory(loc.ID),
		})
	}

	data := struct {
		Locations       []locationRow
		Zip             string
		FavoriteStore   string
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		ServerSignedIn  bool
	}{
		Locations:       rows,
		Zip:             zip,
		FavoriteStore:   favoriteStore,
		ClarityScript:   templates.ClarityScript(),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  serverSignedIn,
	}
	return templates.Location.Execute(w, data)
}
