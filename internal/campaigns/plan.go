package campaigns

import (
	"fmt"
	"net/http"
	"strconv"
)

type planHandler struct {
	campaign landingCampaign
}

func (h planHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	guests, err := strconv.Atoi(query.Get("guests"))
	if err != nil || guests < 1 || guests > 100 {
		http.Error(w, "Tell us how many people are coming (1–100).", http.StatusBadRequest)
		return
	}
	query.Set("instructions", h.campaign.Instructions+fmt.Sprintf("\nCook for %d people. Use this as the guest count when planning portions.", guests))
	query.Del("guests")
	http.Redirect(w, r, "/locations?"+query.Encode(), http.StatusFound)
}
