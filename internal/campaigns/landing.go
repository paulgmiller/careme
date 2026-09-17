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
		Title:        "Good dinners. Smaller grocery bills.",
		Blurb:        "Make your grocery budget go further. Find your local store, then let Careme help you cook satisfying dinners with affordable ingredients, smart swaps, and less food waste.",
		Instructions: "Plan budget-conscious dinners with keeping the total grocery bill low as the top priority. Favor affordable staples, economical proteins, and seasonal produce. Unless dietary preferences require otherwise, include meat, poultry, or fish as a main protein in more than half of the dinners. Choose economical cuts and sensible portions, stretching them with beans, grains, and vegetables rather than making most dinners vegetarian. Reuse ingredients across dinners, make good use of leftovers, and avoid expensive specialty ingredients or one-off purchases. Suggest lower-cost substitutions while keeping meals satisfying and varied. Use available price information to guide choices; do not invent prices or promise specific savings.",
	},
	"fancy": {
		Title:        "Make dinner an occasion.",
		Blurb:        "Date night or a dinner party? Cook something worth gathering for. Find your local store, then let Careme help you stretch your skills and serve a dinner that impresses.",
		Instructions: "Plan fancy dinners for a date night or dinner party. Help me build my cooking skills with approachable techniques that stretch me a little, thoughtful presentation, and dishes that impress someone. Spend a litte more.",
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
