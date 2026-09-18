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
	"vegetarian": {
		Title: "Vegetarian dinners worth gathering for.",
		Blurb: "Put vegetables at the center of dinner. Find your local store, then let Careme help you cook satisfying vegetarian meals with beans, lentils, whole grains, and plenty of variety.",
		Instructions: `Plan dinners that are vegetarian and plant-forward. Exclude meat, poultry, fish, seafood, and ingredients made from them, such as meat stocks and fish sauce. Eggs and dairy are welcome; meals do not need to be vegan.
Build satisfying meals around vegetables, beans, lentils, whole grains, mushrooms, tofu, eggs, dairy, nuts, and seeds rather than trying to imitate meat in every dish. Prioritize substantial mains with enough protein and variety to feel like complete dinners.
Favor naturally vegetarian dishes from cuisines where vegetables and legumes already play a central role.
`,
	},
	"mediterranean": {
		Title: "Bring Mediterranean flavor to your weeknight.",
		Blurb: "Make room for bright herbs, colorful vegetables, and simple, flavorful dinners. Find your local store, then let Careme help you plan meals inspired by Mediterranean cooking.",
		Instructions: `Plan dinners inspired by a Mediterranean eating pattern. Favor vegetables, legumes, whole grains, fish, poultry, olive oil, nuts, herbs, yogurt, and modest amounts of cheese. Use red meat and highly processed foods sparingly.
Prefer simple, flavorful dishes inspired by Mediterranean cuisines rather than generic healthy food. Keep meals practical enough for normal weeknight cooking.
`,
	},
	"low-carb": {
		Title: "Less starch. Plenty to love at dinner.",
		Blurb: "Keep dinner satisfying with protein, vegetables, and flavorful sauces. Find your local store, then let Careme help you plan lower-carb meals that go easy on starch-heavy sides.",
		Instructions: `Plan satisfying lower-carbohydrate dinners by emphasizing protein, vegetables, healthy fats, and flavorful sauces while reducing reliance on bread, pasta, rice, potatoes, and other starch-heavy sides.
Prefer dishes that are naturally low in carbohydrates rather than awkward substitutions. Do not assume strict ketogenic macros or eliminate carbohydrates entirely.
`,
	},
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
