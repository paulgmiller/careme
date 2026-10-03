package campaigns

import (
	"log/slog"
	"net/http"
	"sort"

	"careme/internal/templates"
)

type campaignLink struct {
	Slug string
	landingCampaign
}

type indexHandler struct{}

func (indexHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	links := make([]campaignLink, 0, len(dinnerCampaigns))
	for slug, campaign := range dinnerCampaigns {
		links = append(links, campaignLink{Slug: slug, landingCampaign: campaign})
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Label < links[j].Label })

	data := struct {
		templates.Page
		Campaigns []campaignLink
	}{
		Page:      templates.NewPage(r.Context()),
		Campaigns: links,
	}
	data.Title = "Dinner ideas"
	data.Description = "Find a dinner plan for your week, from budget-friendly and vegetarian meals to something special."
	if err := templates.CampaignIndex.Execute(w, data); err != nil {
		slog.ErrorContext(r.Context(), "campaign index template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
