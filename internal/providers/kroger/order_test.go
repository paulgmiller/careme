package kroger

import (
	"context"
	"encoding/json"
	"errors"
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
	return url.Values{
		"list_key": {"list-hash"}, "location": {`{"id":"70500874","name":"QFC","chain":"QFC"}`},
		"return_url": {"/recipes?h=list-hash#shopping-list-section"},
		"items":      {`[{"id":"food","name":"Milk","needed":"2 cups","buy":2},{"id":"wine","name":"Red wine","needed":"1 bottle"},{"id":"duplicate","name":"More milk"},{"id":"missing","name":"Eggs"},{"name":"Salt","needed":"1 tsp"}]`},
	}
}

func postOrder(handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "https://careme.test/kroger/cart-transfers", strings.NewReader(form.Encode()))
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

func transferResponse(t *testing.T, response *httptest.ResponseRecorder) ordertypes.Transfer {
	t.Helper()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	assert.Empty(t, response.Header().Get("Location"))
	var result ordertypes.Transfer
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	assert.Equal(t, ordertypes.TransferID("list-hash"), result.TransferID)
	return result
}

func getTransfer(handler http.Handler, id string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/kroger/cart-transfers/"+id, nil))
	return response
}

func connectOrder(t *testing.T, handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	transfer := transferResponse(t, postOrder(handler, form))
	require.Equal(t, "pending_login", transfer.Status)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, transfer.CartURL, nil))
	return response
}

func callbackTransfer(t *testing.T, handler http.Handler, response *httptest.ResponseRecorder) ordertypes.Transfer {
	t.Helper()
	require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
	assert.Equal(t, "/recipes?h=list-hash#shopping-list-section", response.Header().Get("Location"))
	return transferResponse(t, getTransfer(handler, ordertypes.TransferID("list-hash")))
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
		assert.Equal(t, []CartItem{{UPC: "food-upc", Quantity: 1}, {UPC: "wine-upc", Quantity: 1}}, payload.Items)
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	mux := orderMux(a)
	form := orderForm()
	// A posted user ID cannot choose another shopper's credentials or transfer key.
	form.Set("user_id", "someone-else")
	result := postOrder(mux, form)
	query := transferResponse(t, result)
	assert.Equal(t, "complete", query.Code)
	assert.Equal(t, 2, query.Added)
	assert.Equal(t, 2, query.Unmatched)
	assert.Equal(t, "https://www.qfc.com/cart", query.CartURL)
	assert.Equal(t, "no-store", result.Header().Get("Cache-Control"))
	assert.Equal(t, "complete", transferResponse(t, postOrder(mux, form)).Code)
	form.Set("items", `[{"id":"new","name":"New product"}]`)
	assert.Equal(t, http.StatusConflict, postOrder(mux, form).Code)
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
			form.Set("location", `{"id":"70500874","chain":"`+scenario.chain+`"}`)
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
			form.Set("items", `[{"id":"new","name":"Changed after authorization started"}]`)
			callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?code=code&state="+url.QueryEscape(destination.Query().Get("state")), nil)
			callback.AddCookie(cookies[0])
			result := httptest.NewRecorder()
			mux.ServeHTTP(result, callback)
			query := callbackTransfer(t, mux, result)
			assert.Equal(t, "complete", query.Code)
			assert.Equal(t, scenario.cart, query.CartURL)
			assert.Equal(t, []CartItem{{UPC: "food-upc", Quantity: 1}, {UPC: "wine-upc", Quantity: 1}}, added)
			assert.Equal(t, -1, result.Result().Cookies()[0].MaxAge)
		})
	}
}

func TestKrogerOrderOAuthRejectsInvalidContinuation(t *testing.T) {
	for _, scenario := range []string{"wrong state", "missing cookie", "different user", "expired", "future", "missing request", "denied", "missing code", "exchange failure", "save failure"} {
		t.Run(scenario, func(t *testing.T) {
			a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("must not write cart"); return nil, nil })
			mux := orderMux(a)
			start := connectOrder(t, mux, orderForm())
			cookie := start.Result().Cookies()[0]
			state := cookie.Value
			var continuation orderContinuation
			require.NoError(t, a.loadJSON(t.Context(), "kroger_order_requests/"+state, &continuation))
			switch scenario {
			case "wrong state":
				state = "wrong"
			case "different user":
				continuation.Request.UserID = "another-user"
			case "expired":
				continuation.IssuedAt = time.Now().Add(-11 * time.Minute)
			case "future":
				continuation.IssuedAt = time.Now().Add(2 * time.Minute)
			case "missing request":
				a.cache = cache.NewInMemoryCache()
			}
			if scenario != "missing request" {
				require.NoError(t, a.saveJSON(t.Context(), "kroger_order_requests/"+cookie.Value, continuation, cache.Unconditional()))
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
			expected := map[string]string{"denied": "denied", "missing code": "missing_code", "exchange failure": "connect_failed", "save failure": "save_failed"}[scenario]
			if expected != "" {
				assert.Equal(t, expected, callbackTransfer(t, mux, result).Code)
			} else {
				assert.Equal(t, http.StatusBadRequest, result.Code)
			}
		})
	}
}

type orderFailCache struct {
	cache.Cache
	prefix       string
	confirmation bool
}

func (c orderFailCache) Put(ctx context.Context, key, value string, options cache.PutOptions) error {
	if strings.HasPrefix(key, c.prefix) && (!c.confirmation || options.Condition == cache.PutUnconditional) {
		return errors.New("cache unavailable")
	}
	return c.Cache.Put(ctx, key, value, options)
}

func TestKrogerOrderWriteOutcomesBlockRetries(t *testing.T) {
	for _, scenario := range []string{"uncertain", "confirmation failed", "legacy complete", "legacy pending", "legacy uncertain"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			a := orderFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				return cartResponse(http.StatusServiceUnavailable, ""), nil
			})
			saveShopper(t, a, time.Now().Add(time.Hour))
			expected := "uncertain"
			if after, ok := strings.CutPrefix(scenario, "legacy "); ok {
				status := after
				require.NoError(t, a.cache.Put(t.Context(), "kroger_cart_transfers/mock-clerk-user-id/list-hash", `{"status":"`+status+`","added":1,"sent":[{"id":"food","quantity":"1 cup"}]}`, cache.Unconditional()))
				expected = status
				if status == "complete" {
					expected = "changed"
				}
			}
			if scenario == "confirmation failed" {
				a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_cart_transfers/", confirmation: true}
				expected = "pending"
			}
			mux := orderMux(a)
			assert.Equal(t, expected, transferResponse(t, postOrder(mux, orderForm())).Code)
			repeat := transferResponse(t, postOrder(mux, orderForm())).Code
			if scenario == "confirmation failed" {
				assert.Equal(t, "pending", repeat)
			} else {
				assert.Equal(t, expected, repeat)
			}
			if strings.HasPrefix(scenario, "legacy ") {
				assert.Zero(t, calls)
			} else {
				assert.Equal(t, 1, calls)
			}
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
	assert.Equal(t, "pending", transferResponse(t, second).Code)
	assert.Equal(t, "complete", transferResponse(t, <-completed).Code)
	assert.Equal(t, int32(1), calls.Load())
}

func TestKrogerOrderValidation(t *testing.T) {
	for _, change := range []struct{ field, value string }{
		{"return_url", "https://evil.test/"},
		{"return_url", "//evil.test/"},
		{"return_url", "/\\evil.test"},
		{"return_url", ""},
		{"location", `{"id":"walmart_123"}`},
		{"location", "broken"},
		{"location", "null"},
		{"location", "{}"},
		{"list_key", "../other"},
		{"list_key", ""},
		{"items", "broken"},
		{"items", "[]"},
		{"items", "null"},
	} {
		t.Run(change.field+change.value, func(t *testing.T) {
			a := newTestOrderHandler(t, &CartClient{})
			form := orderForm()
			form.Set(change.field, change.value)
			assert.Equal(t, http.StatusBadRequest, postOrder(orderMux(a), form).Code)
		})
	}
	a := newTestOrderHandler(t, &CartClient{})
	request := httptest.NewRequest(http.MethodPost, "https://careme.test/kroger/cart-transfers", strings.NewReader(orderForm().Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://evil.test")
	response := httptest.NewRecorder()
	orderMux(a).ServeHTTP(response, request)
	assert.Equal(t, http.StatusForbidden, response.Code)
	a.auth = orderNoSession{AuthClient: auth.DefaultMock()}
	assert.Equal(t, http.StatusUnauthorized, postOrder(orderMux(a), orderForm()).Code)
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
			expected := "complete"
			switch scenario {
			case "refresh":
				saveShopper(t, a, time.Now().Add(-time.Minute))
			case "no matches":
				saveShopper(t, a, time.Now().Add(time.Hour))
				form.Set("items", `[{"name":"Salt"},{"id":"missing","name":"Eggs"}]`)
				expected = "no_matches"
			case "lookup failure":
				saveShopper(t, a, time.Now().Add(time.Hour))
				a.client.HTTPClient = &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
					return cartResponse(http.StatusServiceUnavailable, ""), nil
				})}
				expected = "lookup_failed"
			case "token failure":
				require.NoError(t, a.cache.Put(t.Context(), "kroger_cart_tokens/mock-clerk-user-id", "broken", cache.Unconditional()))
				expected = "connect_failed"
			case "reservation failure":
				saveShopper(t, a, time.Now().Add(time.Hour))
				a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_cart_transfers/"}
				expected = "start_failed"
			case "request save failure":
				a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_order_requests/"}
			}
			response := postOrder(orderMux(a), form)
			if scenario == "request save failure" {
				response = connectOrder(t, orderMux(a), form)
				assert.Equal(t, http.StatusInternalServerError, response.Code)
				return
			}
			assert.Equal(t, expected, transferResponse(t, response).Code)
		})
	}
}
