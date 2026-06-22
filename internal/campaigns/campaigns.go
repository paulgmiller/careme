package campaigns

import (
	"net/http"
	"net/url"

	"careme/internal/routing"
)

var locations = map[string]string{
	"issiquah-carts": "70100658",
	"issaquah-carts": "70100658",
}

// Register adds campaign redirect routes to mux.
func Register(mux routing.Registrar) {
	for campaign, location := range locations {
		mux.HandleFunc("GET /campaigns/"+campaign, redirectToLocation(location))
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
