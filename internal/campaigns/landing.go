package campaigns

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"careme/internal/auth"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
)

type landingCampaign struct {
	Title        string
	Blurb        string
	Instructions string
}

// Add dinner campaigns here; each uses the same landing page.
var dinnerCampaigns = map[string]landingCampaign{
	"budget": {
		Title: "Good dinners. Smaller grocery bills.",
		Blurb: "Make your grocery budget go further. Find your local store, then let Careme help you cook satisfying dinners with affordable ingredients, smart swaps, and less food waste.",
		// This had a problem with doubling down on lentils as a non protein was used in anchor ingredient
		// Could modify menuplan https://github.com/paulgmiller/careme/pull/938
		// Also could benefit from size info https://github.com/paulgmiller/careme/pull/807
		Instructions: `Minimize the total grocery bill as the top priority. Use available prices rather than assumptions about what is usually cheap.
Favor good-value proteins, inexpensive produce, and ingredients that can be reused across multiple dinners. Encourage bulk or value-pack items when they can be spread across distinct meals. Consider the full package cost, not just the amount used in one recipe, and avoid costly one-off ingredients.
Unless dietary preferences require otherwise, use meat, poultry, or fish as the main protein in more than half of dinners. Keep portions economical and stretch proteins with lower-cost ingredients when useful.
When multiple plans work, prefer the one with the lower total grocery spend, even if it repeats some ingredients. Do not invent prices, discounts, or savings.
`,
	},
	"fancy": {
		Title: "Make dinner an occasion.",
		Blurb: "Date night or a dinner party? Cook something worth gathering for. Find your local store, then let Careme help you stretch your skills and serve a dinner that impresses.",
		Instructions: `Plan elevated dinners for date nights or dinner parties. Favor dishes that feel special, look impressive, and introduce approachable techniques that build cooking skills without becoming overly difficult.
Allow somewhat higher-cost ingredients, premium cuts, richer sauces, and more elaborate sides when they meaningfully improve the meal. Prefer thoughtful presentation and restaurant-style touches over expense for its own sake.
When multiple plans work, prefer the one that feels more distinctive and occasion-worthy, even if it costs somewhat more or takes a little longer.
`,
	},
}

type landingUserLookup interface {
	FromRequest(context.Context, *http.Request, auth.AuthClient) (*utypes.User, error)
}

type landingHandler struct {
	campaign   landingCampaign
	users      landingUserLookup
	authClient auth.AuthClient
}

func (h landingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := h.users.FromRequest(ctx, r, h.authClient)
	if err != nil && !errors.Is(err, auth.ErrNoSession) {
		slog.ErrorContext(ctx, "failed to get campaign user", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	data := struct {
		templates.Page
		Campaign       landingCampaign
		User           *utypes.User
		ServerSignedIn bool
	}{
		Page:           templates.NewPage(ctx),
		Campaign:       h.campaign,
		User:           user,
		ServerSignedIn: user != nil,
	}
	data.Title = h.campaign.Title
	data.Description = h.campaign.Blurb
	if err := templates.CampaignLanding.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "campaign template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
