package cocktails

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"

	"careme/internal/kroger"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
	"careme/internal/seasons"
	"careme/internal/templates"
)

type Centroids interface {
	ZipCentroidByZIP(string) (geo.Coordinate, bool)
}

func (s *Server) locationsPage(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Style     seasons.Style
		ZIP       string
		Searched  bool
		Locations []locationtypes.Location
		Error     string
	}{Style: seasons.GetCurrentStyle(), ZIP: strings.TrimSpace(r.URL.Query().Get("zip"))}
	status := http.StatusOK
	if r.URL.Query().Has("zip") {
		coordinate, ok := s.centroids.ZipCentroidByZIP(data.ZIP)
		if !ok {
			data.Error = "Enter a valid US ZIP code to find a store."
			status = http.StatusBadRequest
		} else {
			stores, err := s.locations.GetLocationsByCoordinates(r.Context(), coordinate)
			if err != nil {
				slog.ErrorContext(r.Context(), "find cocktail stores", "error", err)
				data.Error = "We couldn't find stores just now. Please try again."
				status = http.StatusBadGateway
			} else {
				data.Searched = true
				for _, store := range stores {
					if kroger.NewIdentityProvider().IsID(store.ID) {
						data.Locations = append(data.Locations, store)
					}
				}
			}
		}
	}
	var body bytes.Buffer
	if err := templates.CocktailLocations.Execute(&body, data); err != nil {
		slog.ErrorContext(r.Context(), "render cocktail stores", "error", err)
		http.Error(w, "Unable to show cocktail stores.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := body.WriteTo(w); err != nil {
		slog.ErrorContext(r.Context(), "write cocktail stores", "error", err)
	}
}
