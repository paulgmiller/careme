package campaigns

import (
	"net/http"
	"net/url"

	"careme/internal/routing"
)

const issaquahShoppingListHelp = "Welcome to careme the 3 recipes below were genereated ingredients instock in issaquah fred meyers today but if you want anything else just type it in and say try again chef. Add the recipes your like, hide the ones you don't and we'll build out a shopping list"

// Register adds campaign redirect routes to mux.
func Register(mux routing.Registrar) {
	for campaign, location := range AdvertisedRecipeLocations() {
		mux.HandleFunc("GET /c/"+campaign, redirectToLocation(location.ID, helpForCampaign(campaign)))
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

func helpForCampaign(campaign string) string {
	switch campaign {
	case "issaquah":
		return issaquahShoppingListHelp
	default:
		return ""
	}
}

func cloneValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, value := range values {
		cloned[key] = append([]string(nil), value...)
	}
	return cloned
}
