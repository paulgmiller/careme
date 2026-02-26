package locations

import (
	"careme/internal/auth"
	"careme/internal/config"
	"careme/internal/kroger"
	locationtypes "careme/internal/locations/types"
	"careme/internal/seasons"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
	"careme/internal/walmart"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sync"
)

type userLookup interface {
	FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error)
}

type locationStorage struct {
	locationCache map[string]Location
	cacheLock     sync.Mutex // to protect locationMap
	client        []locationBackend
}

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

func New(cfg *config.Config) (locationGetter, error) {
	if cfg.Mocks.Enable {
		return mock{}, nil
	}

	//pass these in?
	kclient, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kroger client: %w", err)
	}
	wclient, err := walmart.NewClient(cfg.Walmart)
	if err != nil {
		return nil, fmt.Errorf("failed to create Walmart client: %w", err)
	}
	return &locationStorage{
		locationCache: make(map[string]Location),
		cacheLock:     sync.Mutex{},
		client:        []locationBackend{kclient, wclient},
	}, nil

}

func NewServer(storage locationGetter, userStorage userLookup) *locationServer {
	return &locationServer{
		storage:     storage,
		userStorage: userStorage,
	}
}

func (l *locationStorage) GetLocationByID(ctx context.Context, locationID string) (*Location, error) {
	l.cacheLock.Lock()

	if loc, exists := l.locationCache[locationID]; exists {
		l.cacheLock.Unlock()
		return &loc, nil
	}
	l.cacheLock.Unlock()

	for _, backend := range l.client {
		if !backend.IsID(locationID) {
			continue
		}

		loc, err := backend.GetLocationByID(ctx, locationID)
		if err != nil {
			return nil, err
		}

		l.cacheLock.Lock()
		l.locationCache[locationID] = *loc
		l.cacheLock.Unlock()
		return loc, nil
	}
	return nil, fmt.Errorf("location ID %s not supported by any backend", locationID)
}

func (l *locationStorage) GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error) {
	var allLocations []Location
	for _, backend := range l.client {
		locations, err := backend.GetLocationsByZip(ctx, zipcode)
		if err != nil {
			slog.ErrorContext(ctx, "error fetching locations from backend", "error", err, "zip", zipcode)
			continue
		}
		allLocations = append(allLocations, locations...)
	}

	l.cacheLock.Lock()
	defer l.cacheLock.Unlock()
	for _, loc := range allLocations {
		l.locationCache[loc.ID] = loc
	}
	return allLocations, nil
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
