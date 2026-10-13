package recipes

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/guest"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func TestMissingCampaignShoppingListOffersGeneration(t *testing.T) {
	for _, location := range []locations.Location{
		{ID: "mnfoodclub_delivery", Name: "MN Food Club", ZipCode: "55401"},
		{ID: "smithbrothersfarms_delivery", Name: "Smith Brothers Farms", ZipCode: "98004"},
	} {
		t.Run(location.ID, func(t *testing.T) {
			t.Parallel()
			generator := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
			s := newTestServer(t, withTestClerk(noSessionAuth{}), withTestGenerator(generator),
				withTestLocationServer(staticLocationLookup{location: &location}))
			query := url.Values{"location": {location.ID}, "date": {"2026-07-29"}, "instructions": {"make it vegetarian"}, "help": {"Pick dinner & enjoy"}}
			rr := httptest.NewRecorder()
			s.handleRecipes(rr, httptest.NewRequest(http.MethodGet, "/recipes?"+query.Encode(), nil))
			require.Equal(t, http.StatusNotFound, rr.Code)
			assert.Contains(t, rr.Body.String(), location.Name)
			assert.Contains(t, rr.Body.String(), "Want to generate one?")
			assert.Contains(t, rr.Body.String(), "Generate my shopping list")
			cookies := rr.Result().Cookies()
			require.Len(t, cookies, 1)
			assert.Equal(t, guest.ShoppingListCookieName, cookies[0].Name)
			assert.Equal(t, "0", cookies[0].Value, "viewing the prompt must not consume the guest allowance")
			p := DefaultParams(&location, time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC))
			p.Instructions = query.Get("instructions")
			_, err := s.ParamsFromCache(t.Context(), p.Hash())
			require.ErrorIs(t, err, cache.ErrNotFound)
			select {
			case <-generator.called:
				t.Fatal("viewing the prompt must not generate recipes")
			default:
			}

			// Submit the actual rendered form to verify its action and hidden fields.
			doc, err := html.Parse(strings.NewReader(rr.Body.String()))
			require.NoError(t, err)
			form := url.Values{}
			var action string
			var walk func(*html.Node)
			walk = func(n *html.Node) {
				attrs := map[string]string{}
				for _, a := range n.Attr {
					attrs[a.Key] = a.Val
				}
				if n.Data == "form" {
					action = attrs["action"]
					assert.Equal(t, "POST", attrs["method"])
				}
				if n.Data == "input" {
					form.Set(attrs["name"], attrs["value"])
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					walk(child)
				}
			}
			walk(doc)
			require.NotEmpty(t, action)
			post := httptest.NewRequest(http.MethodPost, action, strings.NewReader(form.Encode()))
			post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			post.AddCookie(cookies[0])
			generated := httptest.NewRecorder()
			s.handleGenerate(generated, post)
			require.Equal(t, http.StatusSeeOther, generated.Code)
			target, err := url.Parse(generated.Header().Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, p.Hash(), target.Query().Get("h"))
			assert.Equal(t, query.Get("help"), target.Query().Get(QueryArgHelp))
			saved, err := s.ParamsFromCache(t.Context(), p.Hash())
			require.NoError(t, err)
			assert.Equal(t, location.ID, saved.Location.ID)
			assert.Equal(t, "make it vegetarian", saved.Instructions)
			select {
			case <-generator.called:
			case <-time.After(5 * time.Second):
				t.Fatal("submitting the form must start generation")
			}
		})
	}
}

func TestExpiredShoppingListOffersStoreSelection(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, withTestClerk(auth.DefaultMock()))
	rr := httptest.NewRecorder()
	s.handleRecipes(rr, httptest.NewRequest(http.MethodGet, "/recipes?h=expired", nil))
	require.Equal(t, http.StatusNotFound, rr.Code)
	assert.Contains(t, rr.Body.String(), "missing or has expired")
	assert.Contains(t, rr.Body.String(), `href="/locations"`)
	assert.NotContains(t, rr.Body.String(), `method="POST"`)
}
