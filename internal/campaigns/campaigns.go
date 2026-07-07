package campaigns

import (
	"net/http"
	"net/url"

	"careme/internal/routing"
)

// Register adds campaign redirect routes to mux.
func Register(mux routing.Registrar) {
	for name, campaign := range AdvertisedRecipeLocations() {
		mux.HandleFunc("GET /c/"+name, redirectToLocation(campaign.Location.ID, campaign.HelpMessage))
	}
}

func redirectToLocation(location string, helpMessage string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := cloneValues(r.URL.Query())
		query.Set("location", location)
		if helpMessage != "" && query.Get("help") == "" {
			query.Set("help", helpMessage)
		}

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
