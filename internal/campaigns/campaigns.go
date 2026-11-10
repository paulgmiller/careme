package campaigns

import (
	"context"
	"net/http"
	"net/url"

	"careme/internal/auth"
	"careme/internal/recipes"
	"careme/internal/routing"
)

// Register adds campaign landing pages and store redirect routes to mux.
func Register(mux routing.Registrar, users landingUserLookup, authClient auth.AuthClient) {
	for name, campaign := range dinnerCampaigns {
		mux.Handle("GET /c/"+name, landingHandler{
			campaign:   campaign,
			users:      users,
			authClient: authClient,
		})
	}
	for name, campaign := range AdvertisedRecipeLocations() {
		mux.HandleFunc("GET /c/"+name, redirectToLocation(campaign.Location.ID, campaign.HelpMessage))
	}
}

func SitemapUrls(_ context.Context) []string {
	var urls []string
	for key := range dinnerCampaigns {
		urls = append(urls, "/c/"+key)
	}
	return urls
}

func redirectToLocation(location string, helpMessage string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		query.Set("location", location)
		if helpMessage != "" {
			query.Set(recipes.QueryArgHelp, helpMessage)
		}

		target := url.URL{
			Path:     "/recipes",
			RawQuery: query.Encode(),
		}
		http.Redirect(w, r, target.String(), http.StatusFound)
	}
}
