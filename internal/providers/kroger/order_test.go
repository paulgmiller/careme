package kroger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func orderForm() url.Values {
	return url.Values{"idempotency_key": {"list-hash"}, "transfer_request": {`{"location":{"id":"70500874","name":"QFC","chain":"QFC"},"return_url":"/recipes?h=list-hash#shopping-list-section","items":[{"id":"food","name":"Milk","needed":"2 cups","buy":2},{"id":"wine","name":"Red wine","needed":"1 bottle"},{"id":"missing","name":"Eggs"},{"name":"Salt","needed":"1 tsp"}]}`}}
}

func setOrderField(t *testing.T, form url.Values, field string, value any) {
	t.Helper()
	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(form.Get("transfer_request")), &input))
	input[field] = value
	body, err := json.Marshal(input)
	require.NoError(t, err)
	form.Set("transfer_request", string(body))
}

func mustOrderRequest(t *testing.T) ordertypes.TransferRequest {
	t.Helper()
	var request ordertypes.TransferRequest
	require.NoError(t, json.Unmarshal([]byte(orderForm().Get("transfer_request")), &request))
	return request
}

func postOrder(handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "https://careme.test/kroger/order-transfers", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	return result
}

func orderMux(a *OrderHandler) *http.ServeMux {
	mux := http.NewServeMux()
	a.Register(mux)
	return mux
}

func savedResult(t *testing.T, a *OrderHandler, key string) ordertypes.Transfer {
	t.Helper()
	var transfer ordertypes.Transfer
	require.NoError(t, a.loadJSON(t.Context(), transferKey("mock-clerk-user-id", key), &transfer))
	return transfer
}

func transferResponse(t *testing.T, a *OrderHandler, response *httptest.ResponseRecorder) ordertypes.Transfer {
	t.Helper()
	result := savedResult(t, a, "list-hash")
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
	assert.Equal(t, "/recipes?h=list-hash#shopping-list-section", response.Header().Get("Location"))
	return result
}

func connectOrder(t *testing.T, handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	response := postOrder(handler, form)
	require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
	require.NotEmpty(t, response.Result().Cookies())
	return response
}

func orderFixture(t *testing.T, add func(*http.Request) (*http.Response, error)) *OrderHandler {
	t.Helper()
	client := &http.Client{Transport: cartRoundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v1/connect/oauth2/token":
			require.NoError(t, r.ParseForm())
			if r.Form.Get("grant_type") == "refresh_token" {
				assert.Equal(t, "refresh", r.Form.Get("refresh_token"))
			}
			return cartResponse(http.StatusOK, `{"access_token":"shopper","refresh_token":"refresh","expires_in":3600}`), nil
		case "/v1/cart/add":
			return add(r)
		case "/v1/products/missing":
			return cartResponse(http.StatusNotFound, `{}`), nil
		case "/v1/products/wine":
			return cartResponse(http.StatusOK, `{"data":{"items":[{"itemId":"wine-upc"}]}}`), nil
		default:
			assert.Equal(t, "70500874", r.URL.Query().Get("filter.locationId"))
			return cartResponse(http.StatusOK, `{"data":{"items":[{"itemId":"food-upc"}]}}`), nil
		}
	})}
	manager := NewKrogerTokenManager("client", "secret", client)
	manager.token, manager.expiresAt = "catalog", time.Now().Add(time.Hour)
	a := newTestOrderHandler(t, &CartClient{ClientID: "client", RedirectURI: "https://careme.test/kroger/callback", HTTPClient: client, CatalogToken: manager})
	return a
}

func saveShopper(t *testing.T, a *OrderHandler, expires time.Time) {
	t.Helper()
	require.NoError(t, a.saveToken(t.Context(), "mock-clerk-user-id", CartToken{AccessToken: "shopper", RefreshToken: "refresh", ExpiresAt: expires}))
}

func TestKrogerOrderPostsFoodAndWineAndPreventsDuplicates(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		assert.Equal(t, "Bearer shopper", r.Header.Get("Authorization"))
		var payload struct {
			Items []CartItem `json:"items"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, []CartItem{{UPC: "food-upc", Quantity: 2}, {UPC: "wine-upc", Quantity: 1}}, payload.Items)
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	mux := orderMux(a)
	form := orderForm()
	// A posted user ID cannot choose another shopper's credentials or transfer key.
	setOrderField(t, form, "user_id", "someone-else")
	result := postOrder(mux, form)
	query := transferResponse(t, a, result)
	assert.Equal(t, ordertypes.TransferApplied, query.Status)
	assert.Equal(t, []ordertypes.TransferItem{
		{ProductID: "food", Name: "Milk", AcceptedQuantity: 2},
		{ProductID: "wine", Name: "Red wine", AcceptedQuantity: 1},
		{ProductID: "missing", Name: "Eggs", RejectedQuantity: 1, RejectionReason: "product_unavailable"},
		{Name: "Salt", RejectedQuantity: 1, RejectionReason: "missing_product_id"},
	}, query.Items)
	assert.Equal(t, "https://www.qfc.com/cart", query.CartURL)
	assert.Equal(t, "no-store", result.Header().Get("Cache-Control"))
	assert.Equal(t, ordertypes.TransferApplied, transferResponse(t, a, postOrder(mux, form)).Status)
	var original ordertypes.Transfer
	require.NoError(t, a.loadJSON(t.Context(), transferKey("mock-clerk-user-id", "list-hash"), &original))
	setOrderField(t, form, "items", json.RawMessage(`[{"id":"new","name":"New product"}]`))
	setOrderField(t, form, "location", map[string]string{"id": "01400943", "chain": "FREDMEYER"})
	setOrderField(t, form, "return_url", "/recipes?h=different")
	repeatResponse := postOrder(mux, form)
	assert.Equal(t, http.StatusSeeOther, repeatResponse.Code)
	assert.Equal(t, "/recipes?h=different", repeatResponse.Header().Get("Location"))
	assert.Equal(t, query, savedResult(t, a, "list-hash"))
	var repeated ordertypes.Transfer
	require.NoError(t, a.loadJSON(t.Context(), transferKey("mock-clerk-user-id", "list-hash"), &repeated))
	assert.Equal(t, original, repeated, "reposting a key keeps the original result")
	assert.Equal(t, 1, calls)
}

func TestKrogerOrderOAuthResumesPostedItems(t *testing.T) {
	for _, scenario := range []struct{ chain, banner, cart string }{
		{"QFC", "qfc", "https://www.qfc.com/cart"},
		{"FRED", "fredmeyer", "https://www.fredmeyer.com/cart"},
		{"FREDMEYER", "fredmeyer", "https://www.fredmeyer.com/cart"},
		{"Fred Meyer", "fredmeyer", "https://www.fredmeyer.com/cart"},
		{"FRYS", "frys", "https://www.frysfood.com/cart"},
	} {
		t.Run(scenario.chain, func(t *testing.T) {
			var added []CartItem
			a := orderFixture(t, func(r *http.Request) (*http.Response, error) {
				var payload struct {
					Items []CartItem `json:"items"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				added = payload.Items
				return cartResponse(http.StatusNoContent, ""), nil
			})
			mux := orderMux(a)
			form := orderForm()
			setOrderField(t, form, "location", map[string]string{"id": "70500874", "chain": scenario.chain})
			response := connectOrder(t, mux, form)
			require.Equal(t, http.StatusSeeOther, response.Code)
			destination, err := url.Parse(response.Header().Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, scenario.banner, destination.Query().Get("banner"))
			assert.Equal(t, "https://careme.test/kroger/callback", destination.Query().Get("redirect_uri"))
			cookies := response.Result().Cookies()
			require.Len(t, cookies, 1)
			assert.True(t, cookies[0].Secure)
			assert.True(t, cookies[0].HttpOnly)
			assert.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
			// No recipes, location lookup, or shopping merger exists in this fixture.
			setOrderField(t, form, "items", json.RawMessage(`[{"id":"new","name":"Changed after authorization started"}]`))
			callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?code=code&state="+url.QueryEscape(destination.Query().Get("state")), nil)
			callback.AddCookie(cookies[0])
			result := httptest.NewRecorder()
			mux.ServeHTTP(result, callback)
			query := transferResponse(t, a, result)
			assert.Equal(t, ordertypes.TransferApplied, query.Status)
			assert.Equal(t, scenario.cart, query.CartURL)
			assert.Equal(t, []CartItem{{UPC: "food-upc", Quantity: 2}, {UPC: "wine-upc", Quantity: 1}}, added)
			assert.Equal(t, -1, result.Result().Cookies()[0].MaxAge)
		})
	}
}

func TestKrogerOrderOAuthRejectsInvalidContinuation(t *testing.T) {
	for _, scenario := range []string{"wrong state", "missing cookie", "different user", "expired", "future", "missing timestamp", "missing request", "denied", "missing code", "exchange failure", "save failure"} {
		t.Run(scenario, func(t *testing.T) {
			a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("must not write cart"); return nil, nil })
			mux := orderMux(a)
			start := connectOrder(t, mux, orderForm())
			cookie := start.Result().Cookies()[0]
			state := cookie.Value
			var continuation oauthContinuation
			require.NoError(t, a.loadJSON(t.Context(), oauthContinuationKey("mock-clerk-user-id", cookie.Value), &continuation))
			switch scenario {
			case "wrong state":
				state = "wrong"
			case "different user":
				continuation.UserID = "another-user"
			case "expired":
				continuation.IssuedAt = time.Now().Add(-11 * time.Minute)
			case "future":
				continuation.IssuedAt = time.Now().Add(2 * time.Minute)
			case "missing timestamp":
				continuation.IssuedAt = time.Time{}
			case "missing request":
				a.cache = cache.NewInMemoryCache()
			}
			if scenario != "missing request" {
				require.NoError(t, a.saveJSON(t.Context(), oauthContinuationKey("mock-clerk-user-id", cookie.Value), continuation, cache.Unconditional()))
			}
			if scenario == "exchange failure" {
				a.client.HTTPClient = &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
					return cartResponse(http.StatusServiceUnavailable, ""), nil
				})}
			}
			if scenario == "save failure" {
				a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_cart_tokens/"}
			}
			values := url.Values{"state": {state}, "code": {"code"}}
			if scenario == "denied" {
				values.Set("error", "access_denied")
			}
			if scenario == "missing code" {
				values.Del("code")
			}
			callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?"+values.Encode(), nil)
			if scenario != "missing cookie" {
				callback.AddCookie(cookie)
			}
			result := httptest.NewRecorder()
			mux.ServeHTTP(result, callback)
			if scenario == "exchange failure" || scenario == "save failure" {
				assert.Equal(t, http.StatusInternalServerError, result.Code)
			} else {
				assert.Equal(t, http.StatusBadRequest, result.Code)
			}
			exists, err := a.cache.Exists(t.Context(), transferKey("mock-clerk-user-id", "list-hash"))
			require.NoError(t, err)
			assert.False(t, exists, "OAuth errors must not create transfers")
		})
	}
}

type orderFailCache struct {
	cache.Cache
	prefix       string
	confirmation bool
	reservation  bool
}

func (c orderFailCache) Put(ctx context.Context, key, value string, options cache.PutOptions) error {
	if strings.HasPrefix(key, c.prefix) {
		var transfer ordertypes.Transfer
		_ = json.Unmarshal([]byte(value), &transfer)
		fail := !c.confirmation && !c.reservation
		fail = fail || (c.confirmation && options.Condition == cache.PutUnconditional && transfer.Status != "pending")
		fail = fail || (c.reservation && options.Condition == cache.PutUnconditional && transfer.Status == "pending")
		if fail {
			return errors.New("cache unavailable")
		}
	}
	return c.Cache.Put(ctx, key, value, options)
}

func TestKrogerOrderWriteOutcomesBlockRetries(t *testing.T) {
	for _, scenario := range []string{"cart write failed", "confirmation failed"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			a := orderFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				return cartResponse(http.StatusServiceUnavailable, ""), nil
			})
			saveShopper(t, a, time.Now().Add(time.Hour))
			expected := "failed"
			if scenario == "confirmation failed" {
				a.cache = orderFailCache{Cache: a.cache, prefix: transferKey("mock-clerk-user-id", "list-hash"), confirmation: true}
				expected = "pending"
			}
			mux := orderMux(a)
			first := postOrder(mux, orderForm())
			if scenario == "confirmation failed" {
				assert.Equal(t, http.StatusInternalServerError, first.Code)
			} else {
				assert.Equal(t, ordertypes.TransferStatus(expected), transferResponse(t, a, first).Status)
			}
			repeated := transferResponse(t, a, postOrder(mux, orderForm()))
			for _, item := range repeated.Items {
				assert.Zero(t, item.AcceptedQuantity, "failed writes must not report confirmed acceptance")
			}
			repeat := repeated.Status
			if scenario == "confirmation failed" {
				assert.Equal(t, ordertypes.TransferPending, repeat)
			} else {
				assert.Equal(t, ordertypes.TransferStatus(expected), repeat)
			}
			assert.Equal(t, 1, calls)
		})
	}
}

func TestKrogerOrderConcurrentSubmission(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)
		<-release
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	mux := orderMux(a)
	completed := make(chan *httptest.ResponseRecorder, 1)
	go func() { completed <- postOrder(mux, orderForm()) }()
	<-entered
	second := postOrder(mux, orderForm())
	close(release)
	assert.Equal(t, ordertypes.TransferPending, transferResponse(t, a, second).Status)
	assert.Equal(t, ordertypes.TransferApplied, transferResponse(t, a, <-completed).Status)
	assert.Equal(t, int32(1), calls.Load())
}

func TestKrogerOrderValidation(t *testing.T) {
	for _, change := range []struct {
		field string
		value any
	}{
		{"return_url", "https://evil.test/"},
		{"return_url", "//evil.test/"},
		{"return_url", "/\\evil.test"},
		{"return_url", ""},
		{"location", json.RawMessage(`{"id":"walmart_123"}`)},
		{"location", "broken"},
		{"location", nil},
		{"location", json.RawMessage(`{}`)},
		{"items", "broken"},
		{"items", json.RawMessage(`[]`)},
		{"items", nil},
	} {
		t.Run(fmt.Sprintf("%s/%v", change.field, change.value), func(t *testing.T) {
			a := newTestOrderHandler(t, &CartClient{})
			form := orderForm()
			setOrderField(t, form, change.field, change.value)
			assert.Equal(t, http.StatusBadRequest, postOrder(orderMux(a), form).Code)
		})
	}
	a := newTestOrderHandler(t, &CartClient{})
	request := httptest.NewRequest(http.MethodPost, "https://careme.test/kroger/order-transfers", strings.NewReader(orderForm().Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://evil.test")
	response := httptest.NewRecorder()
	orderMux(a).ServeHTTP(response, request)
	assert.Equal(t, http.StatusForbidden, response.Code)
	a.auth = orderNoSession{AuthClient: auth.DefaultMock()}
	assert.Equal(t, http.StatusSeeOther, postOrder(orderMux(a), orderForm()).Code)
}

type orderNoSession struct{ auth.AuthClient }

func (orderNoSession) GetUserIDFromRequest(*http.Request) (string, error) {
	return "", auth.ErrNoSession
}

func TestKrogerOrderRefreshAndPreparationFailures(t *testing.T) {
	for _, scenario := range []string{"refresh", "no matches", "lookup failure", "token failure", "reservation failure", "request save failure"} {
		t.Run(scenario, func(t *testing.T) {
			a := orderFixture(t, func(*http.Request) (*http.Response, error) { return cartResponse(http.StatusNoContent, ""), nil })
			form := orderForm()
			expected := "applied"
			switch scenario {
			case "refresh":
				saveShopper(t, a, time.Now().Add(-time.Minute))
			case "no matches":
				saveShopper(t, a, time.Now().Add(time.Hour))
				setOrderField(t, form, "items", json.RawMessage(`[{"name":"Salt"},{"id":"missing","name":"Eggs"}]`))
				expected = "failed"
			case "lookup failure":
				saveShopper(t, a, time.Now().Add(time.Hour))
				a.client.HTTPClient = &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
					return cartResponse(http.StatusServiceUnavailable, ""), nil
				})}
				expected = "failed"
			case "token failure":
				require.NoError(t, a.cache.Put(t.Context(), "kroger_cart_tokens/mock-clerk-user-id", "broken", cache.Unconditional()))
			case "reservation failure":
				saveShopper(t, a, time.Now().Add(time.Hour))
				a.cache = orderFailCache{Cache: a.cache, prefix: transferKey("mock-clerk-user-id", "list-hash")}
			}
			response := postOrder(orderMux(a), form)
			if scenario == "request save failure" {
				a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_oauth_continuations/"}
				response = postOrder(orderMux(a), form)
				assert.Equal(t, http.StatusInternalServerError, response.Code)
				return
			}
			if scenario == "reservation failure" {
				assert.Equal(t, http.StatusConflict, response.Code)
				return
			}
			if scenario == "token failure" {
				assert.Equal(t, http.StatusInternalServerError, response.Code)
				return
			}
			assert.Equal(t, ordertypes.TransferStatus(expected), transferResponse(t, a, response).Status)
		})
	}
}

func TestKrogerOrderRequiresIdempotencyField(t *testing.T) {
	for _, key := range []string{"", "   ", strings.Repeat("x", 256), "../other"} {
		t.Run(key, func(t *testing.T) {
			a := orderFixture(t, func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid key must not write cart")
				return nil, nil
			})
			form := orderForm()
			form.Set("idempotency_key", key)
			request := httptest.NewRequest(http.MethodPost, "/kroger/order-transfers", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Idempotency-Key", "header-is-not-used")
			response := httptest.NewRecorder()
			orderMux(a).ServeHTTP(response, request)
			assert.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
}

func TestKrogerOrderRejectsMalformedForm(t *testing.T) {
	a := newTestOrderHandler(t, &CartClient{})
	request := httptest.NewRequest(http.MethodPost, "/kroger/order-transfers", strings.NewReader("transfer_request=%"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	orderMux(a).ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestKrogerOrderRejectsInvalidTransferPayload(t *testing.T) {
	for _, payload := range []string{"", "broken", "{", "null", "[]", "{}", "{} {}"} {
		t.Run(payload, func(t *testing.T) {
			a := newTestOrderHandler(t, &CartClient{})
			form := url.Values{"transfer_request": {payload}}
			assert.Equal(t, http.StatusBadRequest, postOrder(orderMux(a), form).Code)
		})
	}
}

func TestKrogerShopperTokenIsReusedAcrossTransfers(t *testing.T) {
	adds, exchanges := 0, 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		adds++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	transport := a.client.HTTPClient.Transport
	a.client.HTTPClient.Transport = cartRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/connect/oauth2/token" {
			exchanges++
		}
		return transport.RoundTrip(r)
	})
	mux := orderMux(a)
	start := connectOrder(t, mux, orderForm())
	cookie := start.Result().Cookies()[0]
	callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?code=code&state="+cookie.Value, nil)
	callback.AddCookie(cookie)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, callback)
	require.Equal(t, http.StatusSeeOther, response.Code)
	second := orderForm()
	second.Set("idempotency_key", "another-list")
	response = postOrder(mux, second)
	assert.Equal(t, http.StatusSeeOther, response.Code)
	assert.Equal(t, "/recipes?h=list-hash#shopping-list-section", response.Header().Get("Location"))
	assert.Equal(t, 2, adds)
	assert.Equal(t, 1, exchanges)
	token, err := a.loadToken(t.Context(), "mock-clerk-user-id")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), token.ExpiresAt, time.Minute)
}
