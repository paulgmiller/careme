package locations

import (
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/seasons"
	"careme/internal/templates"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sync"
)

type krogerClient interface {
	LocationListWithResponse(ctx context.Context, params *kroger.LocationListParams, reqEditors ...kroger.RequestEditorFn) (*kroger.LocationListResponse, error)
	// LocationDetailsWithResponse request
	LocationDetailsWithResponse(ctx context.Context, locationId string, reqEditors ...kroger.RequestEditorFn) (*kroger.LocationDetailsResponse, error)
}

type locationServer struct {
	locationCache map[string]Location
	cacheLock     sync.Mutex // to protect locationMap
	client        krogerClient
}

type locationGetter interface {
	GetLocationByID(ctx context.Context, locationID string) (*Location, error)
	GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error)
}

func New(ctx context.Context, cfg *config.Config) (locationGetter, error) {
	if cfg.Mocks.Enable {
		return mock{}, nil
	}

	client, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kroger client: %w", err)
	}
	return &locationServer{
		locationCache: make(map[string]Location),
		cacheLock:     sync.Mutex{},
		client:        client,
	}, nil
}

func (l *locationServer) GetLocationByID(ctx context.Context, locationID string) (*Location, error) {
	l.cacheLock.Lock()

	if loc, exists := l.locationCache[locationID]; exists {
		l.cacheLock.Unlock()
		return &loc, nil
	}
	l.cacheLock.Unlock()

	resp, err := l.client.LocationDetailsWithResponse(ctx, locationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get location details for ID %s: %w", locationID, err)
	}

	if resp.JSON200 == nil || resp.JSON200.Data == nil {
		return nil, fmt.Errorf("no data found for location ID %s", locationID)
	}

	loc := Location{
		ID:      locationID,
		Name:    *resp.JSON200.Data.Name,
		Address: *resp.JSON200.Data.Address.AddressLine1,
	}
	l.cacheLock.Lock()
	defer l.cacheLock.Unlock()
	l.locationCache[locationID] = loc
	return &loc, nil
}

type Location struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	State   string `json:"state"`
}

func (l *locationServer) GetLocationsByZip(ctx context.Context, zipcode string) ([]Location, error) {
	locparams := &kroger.LocationListParams{
		FilterZipCodeNear: &zipcode,
	}
	resp, err := l.client.LocationListWithResponse(ctx, locparams)
	if err != nil {
		return nil, fmt.Errorf("failed to get location list for zip %s: %w", zipcode, err)
	}
	if resp.JSON200 == nil || len(*resp.JSON200.Data) == 0 {
		fmt.Printf("No locations found for zip code %s\n", zipcode)
		return nil, nil
	}

	var locations []Location
	l.cacheLock.Lock()
	defer l.cacheLock.Unlock()
	for _, loc := range *resp.JSON200.Data {
		loc := Location{
			ID:      *loc.LocationId,
			Name:    *loc.Name,
			Address: *loc.Address.AddressLine1,
			State:   *loc.Address.State,
		}
		l.locationCache[loc.ID] = loc
		locations = append(locations, loc)
	}
	return locations, nil
}

func Register(l locationGetter, mux *http.ServeMux) {
	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		/*_, err := users.FromRequest(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				users.ClearCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			slog.ErrorContext(ctx, "failed to load user for locations", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}*/
		zip := r.URL.Query().Get("zip")
		if zip == "" {
			slog.InfoContext(ctx, "no zip code provided to /locations")
			http.Error(w, "provide a zip code with ?zip=12345", http.StatusBadRequest)
			return
		}
		locs, err := l.GetLocationsByZip(ctx, zip)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get locations for zip", "zip", zip, "error", err)
			http.Error(w, "could not get locations", http.StatusInternalServerError)
			return
		}
		data := struct {
			Locations     []Location
			Zip           string
			ClarityScript template.HTML
			Style         seasons.Style
		}{
			Locations:     locs,
			Zip:           zip,
			ClarityScript: templates.ClarityScript(),
			Style:         seasons.GetCurrentStyle(),
		}
		if err := templates.Location.Execute(w, data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}
