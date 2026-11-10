package main

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCampaignLandingAndInstructions(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	client := newTestClient(t)
	for _, campaign := range []struct{ slug, title string }{
		{"fancy", "Make dinner an occasion."},
		{"budget", "Good dinners. Smaller grocery bills."},
		{"vegetarian", "Vegetarian dinners worth gathering for."},
		{"mediterranean", "Bring Mediterranean flavor to your weeknight."},
		{"low-carb", "Less starch. Plenty to love at dinner."},
	} {
		t.Run(campaign.slug, func(t *testing.T) {
			body := mustGetBody(t, client, srv.URL+"/c/"+campaign.slug)
			assert.Contains(t, body, campaign.title)
			assert.NotContains(t, body, "Your kitchen")
			assert.NotContains(t, body, "Careme will:")
			instructions := extractHiddenValue(t, body, "instructions")
			require.NotEmpty(t, instructions)

			for _, query := range []string{"zip=90005", "lat=34.05&lon=-118.3"} {
				t.Run(query, func(t *testing.T) {
					locationsBody := mustGetBody(t, client, srv.URL+"/locations?"+query+"&instructions="+url.QueryEscape(instructions))
					forms := regexp.MustCompile(`(?s)<form[^>]*action="/recipes"[^>]*>(.*?)</form>`).FindAllStringSubmatch(locationsBody, -1)
					require.NotEmpty(t, forms)
					for _, form := range forms {
						assert.Equal(t, 1, strings.Count(form[1], `name="instructions"`))
						assert.Contains(t, form[1], instructions)
					}
				})
			}
		})
	}
	resp := mustGet(t, client, srv.URL+"/c/missing")
	defer func() { require.NoError(t, resp.Body.Close()) }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
