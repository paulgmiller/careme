package mnfoodclubstub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
	"careme/internal/providers/mnfoodclub"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaremePendingLoginContract(t *testing.T) {
	var handler http.Handler
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(api.Close)
	store := newMemoryStore()
	handler = New(api.URL+"/mnfoodclub-stub", store)
	partner, err := mnfoodclub.NewOrderClient(config.MNFoodClubConfig{OrderAPIURL: api.URL + "/mnfoodclub-stub", OrderAccessToken: testToken}, api.Client())
	require.NoError(t, err)
	c := cache.NewInMemoryCache()
	input := ordertypes.OrderViewInput{UserID: "mock-clerk-user-id", ListKey: "list", Location: locationtypes.Location{ID: mnfoodclub.MNFoodClubLocationID, Chain: "MN Food Club"}, Items: []ai.ShoppingItem{{ProductID: "101", VariantID: "1005", Buy: new(float64(2))}}, ReturnURL: "/recipes?h=list"}
	view, err := mnfoodclub.OrderViewForUser(t.Context(), c, partner, input)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mnfoodclub.NewOrderHandler(c, auth.DefaultMock(), partner).Register(mux)
	r := httptest.NewRequest(http.MethodPost, view.TransferURL, strings.NewReader(url.Values{"idempotency_key": {view.IdempotencyKey}, "transfer_request": {view.TransferRequestJSON}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	require.Equal(t, 303, w.Code, w.Body.String())
	records, err := store.list(t.Context())
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "mock-clerk-user-id", records[0].Request.IdentityHints.CaremeUserID)
	assert.Equal(t, "mnfoodclub", records[0].Request.IdentityHints.Store)
	handoffURL := w.Header().Get("Location")
	require.Contains(t, handoffURL, "/mnfoodclub-stub/handoff/")
	view, err = mnfoodclub.OrderViewForUser(t.Context(), c, partner, input)
	require.NoError(t, err)
	assert.Equal(t, handoffURL, view.ReviewCartURL)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	browser := &http.Client{Transport: api.Client().Transport, Jar: jar}
	login, err := browser.Get(handoffURL)
	require.NoError(t, err)
	body, err := io.ReadAll(login.Body)
	require.NoError(t, err)
	require.NoError(t, login.Body.Close())
	assert.Contains(t, string(body), "Food Club test login")
	cart, err := browser.PostForm(handoffURL, url.Values{"name": {"Contract Chef"}})
	require.NoError(t, err)
	require.NoError(t, cart.Body.Close())
	assert.Equal(t, 200, cart.StatusCode)
	view, err = mnfoodclub.OrderViewForUser(t.Context(), c, partner, input)
	require.NoError(t, err)
	assert.Contains(t, view.ReviewCartURL, "/mnfoodclub-stub/#")
	assert.NotContains(t, view.ReviewCartURL, "/handoff/")
	// The server-to-server client has no cookie jar, so status refresh must read
	// the database result rather than infer authentication from browser cookies.
	statusRequest, err := http.NewRequest(http.MethodGet, api.URL+"/mnfoodclub-stub/v1/cart-transfers/"+strings.TrimPrefix(handoffURL, api.URL+"/mnfoodclub-stub/handoff/"), nil)
	require.NoError(t, err)
	statusRequest.Header.Set("Authorization", "Bearer "+testToken)
	status, err := api.Client().Do(statusRequest)
	require.NoError(t, err)
	defer func() { assert.NoError(t, status.Body.Close()) }()
	var result response
	require.NoError(t, json.NewDecoder(status.Body).Decode(&result))
	assert.Equal(t, "Contract Chef", result.UserID)
	require.Len(t, result.Items, 1)
	assert.Equal(t, 2, result.Items[0].AcceptedQuantity)
}
