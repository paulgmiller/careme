package campaigns

import (
	"net/http"
	"net/url"

	"careme/internal/routing"
)

// Register adds campaign redirect routes to mux.
func Register(mux routing.Registrar) {
	for campaign, location := range AdvertisedRecipeLocations() {
		mux.HandleFunc("GET /c/"+campaign, redirectToLocation(location.ID))
	}
}

func redirectToLocation(location string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := cloneValues(r.URL.Query())
		query.Set("location", location)

		target := url.URL{
			Path:     "/recipes",
			RawQuery: query.Encode(),
		}
		http.Redirect(w, r, target.String(), http.StatusFound)
	}
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, value := range values {
		cloned[key] = append([]string(nil), value...)
	}
	return cloned
}
