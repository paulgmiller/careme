package locations

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"careme/internal/auth"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"

	utypes "careme/internal/users/types"
)

type userLookup interface {
	FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error)
}

type locationServer struct {
	storage     locationStore
	zipFetcher  zipFetcher
	userStorage userLookup
}

type zipFetcher interface {
	NearestZIPToCoordinates(lat, lon float64) (string, bool)
}

func NewServer(storage locationStore, zipFetcher zipFetcher, userStorage userLookup) *locationServer {
	return &locationServer{
		storage:     storage,
		zipFetcher:  zipFetcher,
		userStorage: userStorage,
	}
}

func isHTMXRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

func (l *locationServer) Ready(ctx context.Context) error {
	_, err := l.storage.GetLocationsByZip(ctx, "98005") // magic number is my zip code :)
	return err
}

func (l *locationServer) Register(mux routing.Registrar, authClient auth.AuthClient) {
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

		ctx := r.Context()
		if _, err := l.storage.GetLocationByID(ctx, storeID); err != nil {
			http.Error(w, "invalid store_id", http.StatusBadRequest)
			return
		}
		if l.storage.HasInventory(storeID) {
			http.Error(w, "store already supported", http.StatusBadRequest)
			return
		}

		if err := l.storage.RequestStore(ctx, storeID); err != nil {
			http.Error(w, "failed to submit request", http.StatusInternalServerError)
			return
		}

		if err := templates.Location.ExecuteTemplate(w, "location_request_store_success", nil); err != nil {
			slog.ErrorContext(ctx, "failed to render request-store success fragment", "store_id", storeID, "error", err)
			http.Error(w, "failed to submit request", http.StatusInternalServerError)
			return
		}
	})
}

func (l *locationServer) renderLocationsPage(w http.ResponseWriter, ctx context.Context, zip string, favoriteStore string, serverSignedIn bool) error {
	locs, err := l.storage.GetLocationsByZip(ctx, zip)
	// be very forgiving of errors here.
	if len(locs) == 0 && err != nil {
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
		ClarityScript:   templates.ClarityScript(ctx),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  serverSignedIn,
	}
	return templates.Location.Execute(w, data)
}
