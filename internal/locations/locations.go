package locations

import (
	"careme/internal/auth"
	"careme/internal/config"
	"careme/internal/kroger"
	locationtypes "careme/internal/locations/types"
	"careme/internal/seasons"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
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
	client        locationGetter
}

type locationServer struct {
	storage     locationGetter
	userStorage userLookup
}

type locationGetter interface {
	GetLocationByID(ctx context.Context, locationID string) (*Location, error)
	GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error)
}

// Location is kept as an alias for compatibility with existing imports.
type Location = locationtypes.Location

func New(cfg *config.Config) (locationGetter, error) {
	if cfg.Mocks.Enable {
		return mock{}, nil
	}

	//pass these in?
	client, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kroger client: %w", err)
	}
	return &locationStorage{
		locationCache: make(map[string]Location),
		cacheLock:     sync.Mutex{},
		client:        client,
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

	loc, err := l.client.GetLocationByID(ctx, locationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get location details for ID %s: %w", locationID, err)
	}
	l.cacheLock.Lock()
	defer l.cacheLock.Unlock()
	l.locationCache[locationID] = *loc
	return loc, nil
}

func (l *locationStorage) GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error) {
	locations, err := l.client.GetLocationsByZip(ctx, zipcode)
	if err != nil {
		return nil, fmt.Errorf("failed to get location list for zip %s: %w", zipcode, err)
	}
	l.cacheLock.Lock()
	defer l.cacheLock.Unlock()
	for _, loc := range locations {
		l.locationCache[loc.ID] = loc
	}
	return locations, nil
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
