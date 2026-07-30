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
	"sync"

	"careme/internal/auth"
	"careme/internal/guest"
	"careme/internal/httpx"
	"careme/internal/locations/geo"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"

	utypes "careme/internal/users/types"

	"github.com/samber/lo"
)

type userLookup interface {
	FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error)
}

type locationServer struct {
	storage       locationStore
	zipCentroids  centroidByZip
	userStorage   userLookup
	produceScores produceScoreLookup
}

type produceScoreLookup interface {
	ProduceScore(ctx context.Context, loc Location) *ProduceScore
}

func NewServer(storage locationStore, zipCentroids centroidByZip, userStorage userLookup, produceScores produceScoreLookup) *locationServer {
	return &locationServer{
		storage:       storage,
		zipCentroids:  zipCentroids,
		userStorage:   userStorage,
		produceScores: produceScores,
	}
}

func (l *locationServer) Ready(ctx context.Context) error {
	coordinates, ok := l.zipCentroids.ZipCentroidByZIP("98005") // magic number is my zip code :)
	if !ok {
		return errors.New("readiness ZIP centroid not found")
	}
	_, err := l.storage.GetLocationsByCoordinates(ctx, coordinates)
	return err
}

func (l *locationServer) Register(mux routing.Registrar, authClient auth.AuthClient) {
	mux.HandleFunc("GET /locations/zip-from-coordinates", func(w http.ResponseWriter, r *http.Request) {
		coordinates, err := geo.FromString(r.URL.Query().Get("lat"), r.URL.Query().Get("lon"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		query := url.Values{}
		query.Set("lat", strconv.FormatFloat(coordinates.Lat, 'g', -1, 64))
		query.Set("lon", strconv.FormatFloat(coordinates.Lon, 'g', -1, 64))
		http.Redirect(w, r, "/locations?"+query.Encode(), http.StatusFound)
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
			// give them a few free samples since they came in through locaitons
			guest.EnsureShoppingListCount(w, r)
		}

		coordinates, zip, err := l.searchCoordinates(r)
		if err != nil {
			slog.InfoContext(ctx, "invalid location search", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var favoriteStore string
		if currentUser != nil {
			favoriteStore = currentUser.FavoriteStore
		}
		if err := l.renderLocationsPage(w, ctx, coordinates, zip, favoriteStore, currentUser != nil); err != nil {
			slog.ErrorContext(ctx, "failed to render locations page", "lat", coordinates.Lat, "lon", coordinates.Lon, "error", err)
			http.Error(w, "Failed to render locations page. ", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("POST /locations/request-store", func(w http.ResponseWriter, r *http.Request) {
		if !httpx.IsHTMX(r) {
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

func (l *locationServer) searchCoordinates(r *http.Request) (geo.Coordinate, string, error) {
	query := r.URL.Query()
	zip := strings.TrimSpace(query.Get("zip"))
	lat := strings.TrimSpace(query.Get("lat"))
	lon := strings.TrimSpace(query.Get("lon"))

	if zip != "" {
		if lat != "" || lon != "" {
			return geo.Coordinate{}, "", errors.New("provide either a ZIP code or coordinates, not both")
		}
		coordinates, ok := l.zipCentroids.ZipCentroidByZIP(zip)
		if !ok {
			return geo.Coordinate{}, "", fmt.Errorf("coordinates not found for ZIP code %q", zip)
		}
		return coordinates, zip, nil
	}

	if lat == "" || lon == "" {
		return geo.Coordinate{}, "", errors.New("provide a ZIP code or both latitude and longitude")
	}
	coordinates, err := geo.FromString(lat, lon)
	if err != nil {
		return geo.Coordinate{}, "", err
	}
	return coordinates, "", nil
}

func (l *locationServer) renderLocationsPage(w http.ResponseWriter, ctx context.Context, coordinates geo.Coordinate, zip string, favoriteStore string, serverSignedIn bool) error {
	locs, err := l.storage.GetLocationsByCoordinates(ctx, coordinates)
	// be very forgiving of errors here.
	if len(locs) == 0 && err != nil {
		return fmt.Errorf("failed to get locations near %f,%f: %w", coordinates.Lat, coordinates.Lon, err)
	}

	type locationRow struct {
		Location
		SupportsStaples bool
		ProduceScore    *ProduceScore
	}

	var wg sync.WaitGroup
	rows := make([]*locationRow, 0, len(locs))
	scored := 0
	for _, loc := range locs {
		supportsStaples := l.storage.HasInventory(loc.ID)
		row := &locationRow{
			Location:        loc,
			SupportsStaples: supportsStaples,
		}
		// only do the first 10 rest is a waste.
		if l.produceScores != nil && supportsStaples && scored < 10 {
			scored++
			wg.Go(func() {
				row.ProduceScore = l.produceScores.ProduceScore(ctx, loc)
			})
		}
		rows = append(rows, row)
	}

	wg.Wait()

	data := struct {
		Locations       []locationRow
		FavoriteStore   string
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		ServerSignedIn  bool
	}{
		Locations:       lo.FromSlicePtr(rows),
		FavoriteStore:   favoriteStore,
		ClarityScript:   templates.ClarityScript(ctx),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  serverSignedIn,
	}
	return templates.Location.Execute(w, data)
}
