package campaigns

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"

	"careme/internal/auth"
	"careme/internal/routing"
	"careme/internal/seasons"
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
	"budget-dinners": {
		Title:        "Good dinners. Smaller grocery bills.",
		Blurb:        "Make your grocery budget go further. Find your local store, then let Careme help you cook satisfying dinners with affordable ingredients, smart swaps, and less food waste.",
		Instructions: "Plan budget-conscious dinners with keeping the total grocery bill low as the top priority. Favor affordable staples, economical proteins, and seasonal produce. Reuse ingredients across dinners, make good use of leftovers, and avoid expensive specialty ingredients or one-off purchases. Suggest lower-cost substitutions while keeping meals satisfying and varied. Use available price information to guide choices; do not invent prices or promise specific savings.",
	},
	"fancy-dinners": {
		Title:        "Make dinner an occasion.",
		Blurb:        "Date night or a dinner party? Cook something worth gathering for. Find your local store, then let Careme help you stretch your skills and serve a dinner that impresses.",
		Instructions: "Plan fancy dinners for a date night or dinner party. Help me build my cooking skills with approachable techniques that stretch me a little, thoughtful presentation, and dishes that impress someone.",
	},
}

type landingUserLookup interface {
	FromRequest(context.Context, *http.Request, auth.AuthClient) (*utypes.User, error)
}

// RegisterLanding adds campaign pages that start with a local store search.
func RegisterLanding(routes routing.Registrar, users landingUserLookup, authClient auth.AuthClient) {
	routes.HandleFunc("GET /c/{campaign}", func(w http.ResponseWriter, r *http.Request) {
		c, ok := dinnerCampaigns[r.PathValue("campaign")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		ctx := r.Context()
		user, err := users.FromRequest(ctx, r, authClient)
		if err != nil && !errors.Is(err, auth.ErrNoSession) {
			slog.ErrorContext(ctx, "failed to get campaign user", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		data := struct {
			Campaign        *landingCampaign
			ClarityScript   template.HTML
			GoogleTagScript template.HTML
			User            *utypes.User
			Style           seasons.Style
			ServerSignedIn  bool
		}{
			Campaign:        &c,
			ClarityScript:   templates.ClarityScript(ctx),
			GoogleTagScript: templates.GoogleTagScript(),
			User:            user,
			Style:           seasons.GetCurrentStyle(),
			ServerSignedIn:  user != nil,
		}
		if err := templates.Home.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "campaign template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}
