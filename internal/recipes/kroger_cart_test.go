package recipes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/providers/kroger"
	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type krogerTestTransport func(*http.Request) (*http.Response, error)

func (f krogerTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func krogerTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestKrogerCartAuthorizationUsesStoreBanner(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chain  string
		banner string
	}{
		{name: "Bellevue", chain: "QFC", banner: "qfc"},
		{name: "QFC Bellevue", chain: "KROGER", banner: "kroger"},
		{name: "Redmond", chain: "FREDMEYER", banner: "fredmeyer"},
		{name: "Kroger on the Rhine", chain: "Kroger", banner: "kroger"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			testKrogerAdapter(t, s).Client = &kroger.CartClient{ClientID: "client-id", RedirectURI: "https://careme.test/kroger/callback"}
			p := DefaultParams(&locations.Location{ID: "70500874", Name: tc.name, Chain: tc.chain}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
			s.locServer = staticLocationLookup{location: p.Location}
			require.NoError(t, s.SaveParams(t.Context(), p))
			cacheKrogerTestPurchases(t, s, p)
			req := httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/kroger-cart", nil)
			req.SetPathValue("hash", p.Hash())
			rr := httptest.NewRecorder()
			s.handleKrogerCart(rr, req)
			require.Equal(t, http.StatusSeeOther, rr.Code)
			redirect, err := url.Parse(rr.Header().Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, "api.kroger.com", redirect.Host)
			assert.Equal(t, "/v1/connect/oauth2/authorize", redirect.Path)
			assert.Equal(t, tc.banner, redirect.Query().Get("banner"))
			assert.Equal(t, "client-id", redirect.Query().Get("client_id"))
			assert.Equal(t, testKrogerAdapter(t, s).Client.RedirectURI, redirect.Query().Get("redirect_uri"))
			assert.Equal(t, "cart.basic:write", redirect.Query().Get("scope"))
			require.Len(t, rr.Result().Cookies(), 1)
			body, err := base64.RawURLEncoding.DecodeString(rr.Result().Cookies()[0].Value)
			require.NoError(t, err)
			var state krogerAuthState
			require.NoError(t, json.Unmarshal(body, &state))
			assert.NotEmpty(t, state.Nonce)
			assert.Equal(t, state.Nonce, redirect.Query().Get("state"))
			assert.Equal(t, p.Hash(), state.Hash)
		})
	}
}

func TestKrogerCartTransferAndEncryptedConnection(t *testing.T) {
	s := newTestServer(t)
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	setTestKrogerKey(t, s, key)
	token := kroger.CartToken{AccessToken: "shopper-secret", RefreshToken: "refresh-secret", ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, s.saveCartToken(t.Context(), "shopper", token))
	loaded, err := s.loadCartToken(t.Context(), "shopper")
	require.NoError(t, err)
	assert.Equal(t, token.AccessToken, loaded.AccessToken)
	assert.Equal(t, token.RefreshToken, loaded.RefreshToken)
	assert.True(t, token.ExpiresAt.Equal(loaded.ExpiresAt))
	reader, err := s.Cache.Get(t.Context(), s.cartTokenKey("shopper"))
	require.NoError(t, err)
	stored, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.NotContains(t, string(stored), "shopper-secret")
	assert.NotContains(t, string(stored), "refresh-secret")
	assert.NotEmpty(t, stored)

	var cartRequests int
	httpClient := &http.Client{Transport: krogerTestTransport(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/connect/oauth2/token":
			return krogerTestResponse(http.StatusOK, `{"access_token":"catalog","expires_in":3600}`), nil
		case "/v1/products/0001111060903":
			assert.Equal(t, "01400943", req.URL.Query().Get("filter.locationId"))
			return krogerTestResponse(http.StatusOK, `{"data":{"items":[{"itemId":"upc-1"}]}}`), nil
		case "/v1/cart/add":
			cartRequests++
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			assert.JSONEq(t, `{"items":[{"upc":"upc-1","quantity":1}]}`, string(body))
			return krogerTestResponse(http.StatusNoContent, ""), nil
		default:
			t.Fatalf("unexpected Kroger request: %s", req.URL)
			return nil, nil
		}
	})}
	testKrogerAdapter(t, s).Client = &kroger.CartClient{ClientID: "id", ClientSecret: "secret", HTTPClient: httpClient, CatalogToken: kroger.NewKrogerTokenManager("id", "secret", httpClient)}
	p := DefaultParams(&locations.Location{ID: "01400943", Chain: "Kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Garlic dish", Ingredients: []ai.Ingredient{{ProductID: "0001111060903", Name: "Garlic", Quantity: "2 cloves"}, {Name: "Salt", Quantity: "1 tsp"}}}}
	s.locServer = staticLocationLookup{location: p.Location}
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		return []ai.ShoppingItem{{ProductID: "0001111060903", Name: "Garlic", Needed: "3 cloves"}, {Name: "Salt", Needed: "1 tsp"}}, nil
	})
	require.NoError(t, s.SaveParams(t.Context(), p))
	cacheKrogerTestPurchases(t, s, p)
	rr := httptest.NewRecorder()
	s.performCartTransfer(rr, httptest.NewRequest(http.MethodPost, "/recipes/x/kroger-cart", nil), "mock-clerk-user-id", p.Hash(), token)
	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "https://www.kroger.com/cart", rr.Header().Get("Location"))
	page := renderKrogerShoppingSection(t, s, p.Hash(), "")
	assert.Contains(t, page, "Sent 1 product")
	assert.Contains(t, page, "Garlic — 1 package")
	assert.Contains(t, page, "Not sent to your cart")
	assert.Contains(t, page, "Salt")
	assert.Contains(t, page, `target="_blank"`)
	assert.Contains(t, page, `rel="noopener noreferrer"`)
	assert.NotContains(t, page, "Add to Kroger cart")
	result, err := s.loadCartTransfer(t.Context(), "mock-clerk-user-id", p.Hash())
	require.NoError(t, err)
	assert.Equal(t, "3 cloves", result.Sent[0].Quantity, "transfer uses the finalized merged snapshot")
	assert.Equal(t, "complete", result.Status)
	assert.Equal(t, 1, result.Added)
	assert.Equal(t, 1, cartRequests)
	result.Fingerprint = "older-list"
	require.NoError(t, s.saveCartTransfer(t.Context(), "mock-clerk-user-id", p.Hash(), result, cache.Unconditional()))
	repeatReq := httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/kroger-cart", nil)
	repeatReq.SetPathValue("hash", p.Hash())
	repeat := httptest.NewRecorder()
	s.handleKrogerCart(repeat, repeatReq)
	assert.Equal(t, http.StatusSeeOther, repeat.Code)
	assert.Contains(t, renderKrogerShoppingSection(t, s, p.Hash(), ""), "shopping list changed")
	assert.Equal(t, 1, cartRequests)
}

func TestKrogerCallback(t *testing.T) {
	for _, tc := range []struct {
		name         string
		changeState  func(*krogerAuthState)
		cookieValue  string
		omitCookie   bool
		query        string
		tokenStatus  int
		wantStatus   int
		wantMessage  string
		wantExchange bool
	}{
		{name: "connect and transfer", query: "state=nonce&code=code", wantStatus: http.StatusSeeOther, wantExchange: true},
		{name: "missing cookie", omitCookie: true, wantStatus: http.StatusBadRequest, wantMessage: "connection expired"},
		{name: "invalid encoding", cookieValue: "!", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "invalid JSON", cookieValue: base64.RawURLEncoding.EncodeToString([]byte("invalid")), wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "wrong account", changeState: func(state *krogerAuthState) { state.UserID = "other" }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "wrong state", query: "state=other&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "missing shopping list", changeState: func(state *krogerAuthState) { state.Hash = "" }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "missing timestamp", changeState: func(state *krogerAuthState) { state.IssuedAt = time.Time{} }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "expired state", changeState: func(state *krogerAuthState) { state.IssuedAt = time.Now().Add(-11 * time.Minute) }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "future state", changeState: func(state *krogerAuthState) { state.IssuedAt = time.Now().Add(2 * time.Minute) }, query: "state=nonce&code=code", wantStatus: http.StatusBadRequest, wantMessage: "Invalid Kroger connection"},
		{name: "denied", query: "state=nonce&error=access_denied", wantStatus: http.StatusSeeOther, wantMessage: "not approved"},
		{name: "missing code", query: "state=nonce", wantStatus: http.StatusSeeOther, wantMessage: "did not finish"},
		{name: "exchange failed", query: "state=nonce&code=code", tokenStatus: http.StatusUnauthorized, wantStatus: http.StatusSeeOther, wantMessage: "Unable to connect", wantExchange: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			setTestKrogerKey(t, s, make([]byte, 32))
			p := DefaultParams(&locations.Location{ID: "01400943", Chain: "Kroger"}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "garlic", Name: "Garlic", Quantity: "2 cloves"}}}}
			s.locServer = staticLocationLookup{location: p.Location}
			require.NoError(t, s.SaveParams(t.Context(), p))
			cacheKrogerTestPurchases(t, s, p)
			var exchanges, additions int
			client := &http.Client{Transport: krogerTestTransport(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/v1/connect/oauth2/token":
					require.NoError(t, req.ParseForm())
					if req.Form.Get("grant_type") == "authorization_code" {
						exchanges++
						assert.Equal(t, "code", req.Form.Get("code"))
						assert.Equal(t, "https://test.careme.cooking/kroger/callback", req.Form.Get("redirect_uri"))
						if tc.tokenStatus != 0 {
							return krogerTestResponse(tc.tokenStatus, ""), nil
						}
						return krogerTestResponse(http.StatusOK, `{"access_token":"shopper","refresh_token":"refresh","expires_in":3600}`), nil
					}
					return krogerTestResponse(http.StatusOK, `{"access_token":"catalog","expires_in":3600}`), nil
				case "/v1/products/garlic":
					return krogerTestResponse(http.StatusOK, `{"data":{"items":[{"itemId":"garlic-upc"}]}}`), nil
				case "/v1/cart/add":
					additions++
					assert.Equal(t, "Bearer shopper", req.Header.Get("Authorization"))
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					assert.JSONEq(t, `{"items":[{"upc":"garlic-upc","quantity":1}]}`, string(body))
					return krogerTestResponse(http.StatusNoContent, ""), nil
				default:
					t.Fatalf("unexpected Kroger request: %s", req.URL)
					return nil, nil
				}
			})}
			testKrogerAdapter(t, s).Client = &kroger.CartClient{ClientID: "id", ClientSecret: "secret", RedirectURI: "https://test.careme.cooking/kroger/callback", HTTPClient: client, CatalogToken: kroger.NewKrogerTokenManager("id", "secret", client)}
			state := krogerAuthState{Nonce: "nonce", Hash: p.Hash(), UserID: "mock-clerk-user-id", IssuedAt: time.Now()}
			if tc.changeState != nil {
				tc.changeState(&state)
			}
			body, err := json.Marshal(state)
			require.NoError(t, err)
			value := base64.RawURLEncoding.EncodeToString(body)
			if tc.cookieValue != "" {
				value = tc.cookieValue
			}
			req := httptest.NewRequest(http.MethodGet, "/kroger/callback?"+tc.query, nil)
			if !tc.omitCookie {
				req.AddCookie(&http.Cookie{Name: "careme_kroger_state", Value: value})
			}
			rr := httptest.NewRecorder()
			s.handleKrogerCallback(rr, req)
			assert.Equal(t, tc.wantStatus, rr.Code)
			if tc.wantStatus == http.StatusSeeOther {
				location, err := url.Parse(rr.Header().Get("Location"))
				require.NoError(t, err)
				if tc.name == "connect and transfer" {
					assert.Equal(t, "https://www.kroger.com/cart", location.String())
				} else {
					assert.Equal(t, "/recipes", location.Path)
					assert.Equal(t, p.Hash(), location.Query().Get("h"))
					assert.Equal(t, "shopping-list-section", location.Fragment)
				}
				assert.Empty(t, location.Query().Get("code"))
				assert.Empty(t, location.Query().Get("state"))
				assert.Contains(t, renderKrogerShoppingSection(t, s, p.Hash(), location.Query().Get("kroger_error")), tc.wantMessage)
			} else {
				assert.Contains(t, rr.Body.String(), tc.wantMessage)
			}
			assert.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
			assert.Equal(t, "no-referrer", rr.Header().Get("Referrer-Policy"))
			assert.Equal(t, tc.wantExchange, exchanges == 1)
			if tc.name == "connect and transfer" {
				assert.Equal(t, 1, additions)
				token, err := s.loadCartToken(t.Context(), state.UserID)
				require.NoError(t, err)
				assert.Equal(t, "refresh", token.RefreshToken)
			} else {
				assert.Zero(t, additions)
			}
			if !tc.omitCookie {
				cookies := rr.Result().Cookies()
				require.Len(t, cookies, 1)
				assert.Equal(t, -1, cookies[0].MaxAge)
			}
		})
	}
}

func cacheKrogerTestPurchases(t *testing.T, s *server, p *generatorParams) {
	t.Helper()
	ingredients, catalog, err := s.finalizedShoppingInputs(t.Context(), p.Hash())
	require.NoError(t, err)
	require.NoError(t, s.mergeAndCacheShoppingList(t.Context(), p.Hash(), ingredients, catalog))
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		t.Fatal("Kroger page and transfer must not invoke the shopping merger")
		return nil, nil
	})
}

func renderKrogerShoppingSection(t *testing.T, s *server, hash, errorCode string) string {
	t.Helper()
	p, err := s.ParamsFromCache(t.Context(), hash)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/recipes?h="+url.QueryEscape(hash)+"&kroger_error="+url.QueryEscape(errorCode), nil)
	rr := httptest.NewRecorder()
	user := &utypes.User{ID: "mock-clerk-user-id", Email: []string{"chef@example.test"}}
	s.renderShoppingList(rr, req, p, &ai.ShoppingList{Recipes: p.Saved}, user, shoppingProgress{})
	require.Equal(t, http.StatusOK, rr.Code)
	return rr.Body.String()
}

func TestKrogerTransferReturnToShoppingList(t *testing.T) {
	for _, tc := range []struct {
		name          string
		productStatus int
		cartStatus    int
		wantStatus    string
		wantNotice    string
		wantCartCalls int
	}{
		{name: "no matches", productStatus: http.StatusNotFound, wantStatus: "no_matches", wantNotice: "Nothing was sent to Kroger"},
		{name: "uncertain cart result", cartStatus: http.StatusServiceUnavailable, wantStatus: "uncertain", wantNotice: "could not confirm", wantCartCalls: 1},
		{name: "product lookup failed", productStatus: http.StatusServiceUnavailable, wantNotice: "Nothing was sent. Try again, chef."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			p := DefaultParams(&locations.Location{ID: "01400943", Chain: "Kroger"}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "garlic", Name: "Garlic", Quantity: "2 cloves"}}}}
			s.locServer = staticLocationLookup{location: p.Location}
			require.NoError(t, s.SaveParams(t.Context(), p))
			cacheKrogerTestPurchases(t, s, p)
			cartCalls := 0
			client := &http.Client{Transport: krogerTestTransport(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/v1/connect/oauth2/token":
					return krogerTestResponse(http.StatusOK, `{"access_token":"catalog","expires_in":3600}`), nil
				case "/v1/products/garlic":
					assert.Equal(t, p.Location.ID, req.URL.Query().Get("filter.locationId"))
					if tc.productStatus != 0 {
						return krogerTestResponse(tc.productStatus, ""), nil
					}
					return krogerTestResponse(http.StatusOK, `{"data":{"items":[{"itemId":"garlic-upc"}]}}`), nil
				case "/v1/cart/add":
					cartCalls++
					return krogerTestResponse(tc.cartStatus, ""), nil
				default:
					t.Fatalf("unexpected Kroger request: %s", req.URL)
					return nil, nil
				}
			})}
			testKrogerAdapter(t, s).Client = &kroger.CartClient{HTTPClient: client, CatalogToken: kroger.NewKrogerTokenManager("id", "secret", client)}
			req := httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/kroger-cart", nil)
			rr := httptest.NewRecorder()
			s.performCartTransfer(rr, req, "mock-clerk-user-id", p.Hash(), kroger.CartToken{AccessToken: "shopper"})
			require.Equal(t, http.StatusSeeOther, rr.Code)
			location, err := url.Parse(rr.Header().Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, p.Hash(), location.Query().Get("h"))
			page := renderKrogerShoppingSection(t, s, p.Hash(), location.Query().Get("kroger_error"))
			assert.Contains(t, page, tc.wantNotice)
			assert.Contains(t, page, "Review Kroger cart")
			assert.Equal(t, tc.wantCartCalls, cartCalls)
			if tc.wantStatus == "no_matches" {
				_, err := s.loadCartTransfer(t.Context(), "mock-clerk-user-id", p.Hash())
				require.ErrorIs(t, err, cache.ErrNotFound)
				assert.Contains(t, page, "Add to Kroger cart")
			} else if tc.wantStatus != "" {
				result, err := s.loadCartTransfer(t.Context(), "mock-clerk-user-id", p.Hash())
				require.NoError(t, err)
				assert.Equal(t, tc.wantStatus, result.Status)
				assert.NotContains(t, page, "Add to Kroger cart")
				req.SetPathValue("hash", p.Hash())
				s.handleKrogerCart(httptest.NewRecorder(), req)
				assert.Equal(t, tc.wantCartCalls, cartCalls, "returning to the list must not repeat a transfer")
			} else {
				assert.Contains(t, page, "Add to Kroger cart")
			}
		})
	}
}

func TestKrogerTransferFeedbackIsPrivate(t *testing.T) {
	s := newTestServer(t)
	testKrogerAdapter(t, s).Client = &kroger.CartClient{}
	p := DefaultParams(&locations.Location{ID: "01400943", Chain: "Kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
	s.locServer = staticLocationLookup{location: p.Location}
	require.NoError(t, s.SaveParams(t.Context(), p))
	cacheKrogerTestPurchases(t, s, p)
	require.NoError(t, s.saveCartTransfer(t.Context(), "another-user", p.Hash(), krogerTransfer{Status: "complete", Added: 10, Sent: []ai.Ingredient{{Name: "Private product"}}}, cache.Unconditional()))
	page := renderKrogerShoppingSection(t, s, p.Hash(), "<script>alert(1)</script>")
	assert.NotContains(t, page, "Private product")
	assert.NotContains(t, page, "Sent 10 products")
	assert.NotContains(t, page, "alert(1)")
	assert.Contains(t, page, "Add to Kroger cart")
}

func TestFinalizedKrogerPurchasesUsesProviderID(t *testing.T) {
	for _, tc := range []struct {
		id      string
		chain   string
		wantErr bool
	}{
		{id: "70500874", chain: "QFC"},
		{id: "01400943", chain: "KROGER"},
		{id: "70100123", chain: "FREDMEYER"},
		{id: "walmart_123", chain: "Kroger", wantErr: true},
	} {
		t.Run(tc.id+tc.chain, func(t *testing.T) {
			s := newTestServer(t)
			p := DefaultParams(&locations.Location{ID: tc.id, Chain: tc.chain}, time.Now())
			p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
			s.locServer = staticLocationLookup{location: p.Location}
			require.NoError(t, s.SaveParams(t.Context(), p))
			cacheKrogerTestPurchases(t, s, p)
			purchases, location, err := s.finalizedKrogerPurchases(t.Context(), p.Hash())
			if tc.wantErr {
				require.ErrorContains(t, err, "not a finalized Kroger list")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.chain, location.Chain)
			require.Len(t, purchases, 1)
			assert.Equal(t, "Garlic", purchases[0].Items[0].Name)
		})
	}
}

func TestQFCTransferLinksToQFCCart(t *testing.T) {
	s := newTestServer(t)
	testKrogerAdapter(t, s).Client = &kroger.CartClient{}
	p := DefaultParams(&locations.Location{ID: "70500874", Name: "Bellevue", Chain: "QFC"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
	s.locServer = staticLocationLookup{location: p.Location}
	require.NoError(t, s.SaveParams(t.Context(), p))
	cacheKrogerTestPurchases(t, s, p)
	require.NoError(t, s.saveCartTransfer(t.Context(), "mock-clerk-user-id", p.Hash(), krogerTransfer{Status: "complete", Added: 1, Sent: []ai.Ingredient{{Name: "Garlic"}}}, cache.Unconditional()))
	page := renderKrogerShoppingSection(t, s, p.Hash(), "")
	assert.Contains(t, page, `href="https://www.qfc.com/cart"`)
	assert.Contains(t, page, "Review QFC cart")
	assert.NotContains(t, page, "www.kroger.com/cart")
}

func TestLegacyQFCPlanUsesCurrentChainForCartAndAuthorization(t *testing.T) {
	s := newTestServer(t, withTestLocationServer(staticLocationLookup{location: &locations.Location{ID: "70500874", Name: "QFC Bellevue", Chain: "QFC"}}))
	testKrogerAdapter(t, s).Client = &kroger.CartClient{ClientID: "client", RedirectURI: "https://careme.test/kroger/callback"}
	p := DefaultParams(&locations.Location{ID: "70500874", Name: "QFC Bellevue", Chain: "kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	cacheKrogerTestPurchases(t, s, p)
	page := renderKrogerShoppingSection(t, s, p.Hash(), "denied")
	assert.Contains(t, page, `href="https://www.qfc.com/cart"`)
	assert.Contains(t, page, "Review QFC cart")
	assert.NotContains(t, page, "www.kroger.com/cart")
	req := httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/kroger-cart", nil)
	req.SetPathValue("hash", p.Hash())
	rr := httptest.NewRecorder()
	s.handleKrogerCart(rr, req)
	require.Equal(t, http.StatusSeeOther, rr.Code)
	redirect, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "qfc", redirect.Query().Get("banner"))
}

type cartLocationLookupFunc func(context.Context, string) (*locations.Location, error)

func (f cartLocationLookupFunc) GetLocationByID(ctx context.Context, id string) (*locations.Location, error) {
	return f(ctx, id)
}

func TestKrogerCartRejectsFailedCurrentLocationLookup(t *testing.T) {
	lookupErr := errors.New("location API unavailable")
	s := newTestServer(t, withTestLocationServer(cartLocationLookupFunc(func(_ context.Context, id string) (*locations.Location, error) {
		assert.Equal(t, "70500874", id)
		return nil, lookupErr
	})))
	testKrogerAdapter(t, s).Client = &kroger.CartClient{}
	p := DefaultParams(&locations.Location{ID: "70500874", Chain: "kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
	require.NoError(t, s.SaveParams(t.Context(), p))
	cacheKrogerTestPurchases(t, s, p)
	_, _, err := s.finalizedKrogerPurchases(t.Context(), p.Hash())
	require.ErrorIs(t, err, lookupErr)
	require.ErrorContains(t, err, "load current Kroger location")
	for _, handler := range []http.HandlerFunc{s.handleKrogerCart} {
		req := httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/kroger-cart", nil)
		req.SetPathValue("hash", p.Hash())
		rr := httptest.NewRecorder()
		handler(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
		assert.Empty(t, rr.Header().Get("Location"))
		assert.NotContains(t, rr.Body.String(), "www.kroger.com/cart")
	}
}

func TestKrogerCartLink(t *testing.T) {
	for _, tc := range []struct {
		name  string
		url   string
		brand string
	}{
		{name: "QFC", url: "https://www.qfc.com/cart", brand: "QFC"},
		{name: "qfc", url: "https://www.qfc.com/cart", brand: "QFC"},
		{name: "KROGER", url: "https://www.kroger.com/cart", brand: "Kroger"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, brand := krogerCartLink(tc.name)
			assert.Equal(t, tc.url, url)
			assert.Equal(t, tc.brand, brand)
		})
	}
}

func TestKrogerTransferRequiresFinalizedPurchaseSnapshot(t *testing.T) {
	s := newTestServer(t)
	testKrogerAdapter(t, s).Client = &kroger.CartClient{}
	p := DefaultParams(&locations.Location{ID: "01400943", Chain: "Kroger"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{ProductID: "garlic", Name: "Garlic", Quantity: "2 cloves"}}}}
	s.locServer = staticLocationLookup{location: p.Location}
	require.NoError(t, s.SaveParams(t.Context(), p))
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		t.Fatal("cart transfers must not create a missing finalized snapshot")
		return nil, nil
	})
	rr := httptest.NewRecorder()
	s.performCartTransfer(rr, httptest.NewRequest(http.MethodPost, "/recipes/x/kroger-cart", nil), "mock-clerk-user-id", p.Hash(), kroger.CartToken{AccessToken: "shopper"})
	require.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "kroger_error=list_unavailable")
	_, err := s.loadCartTransfer(t.Context(), "mock-clerk-user-id", p.Hash())
	require.ErrorIs(t, err, cache.ErrNotFound)
}

func TestKrogerPurchasesAppendLateWineWithoutMerging(t *testing.T) {
	s := newTestServer(t)
	p := DefaultParams(&locations.Location{ID: "70500874", Chain: "QFC"}, time.Now())
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}}}}
	s.locServer = staticLocationLookup{location: p.Location}
	require.NoError(t, s.SaveParams(t.Context(), p))
	cacheKrogerTestPurchases(t, s, p)
	before, _, err := s.finalizedKrogerPurchases(t.Context(), p.Hash())
	require.NoError(t, err)
	first := ai.Ingredient{ProductID: "wine", Name: "Red wine", Quantity: "1 bottle"}
	require.NoError(t, s.SaveWine(t.Context(), p.Saved[0].ComputeHash(), &ai.WineSelection{Wines: []ai.Ingredient{first, {ProductID: "other", Name: "Other wine"}}}))
	after, _, err := s.finalizedKrogerPurchases(t.Context(), p.Hash())
	require.NoError(t, err)
	require.Len(t, after, len(before)+1)
	assert.Equal(t, first.ProductID, after[len(after)-1].Items[0].ProductID)
	assert.Equal(t, first.Quantity, after[len(after)-1].Items[0].Needed)
	oldFingerprint, err := krogerPurchaseFingerprint(before)
	require.NoError(t, err)
	newFingerprint, err := krogerPurchaseFingerprint(after)
	require.NoError(t, err)
	assert.NotEqual(t, oldFingerprint, newFingerprint)
	cached, err := s.cachedShoppingPurchases(t.Context(), p.Hash())
	require.NoError(t, err)
	assert.Equal(t, before, cached, "late wine must not alter the finalized food snapshot")
}
