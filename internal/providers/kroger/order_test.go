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
	request := httptest.NewRequest(http.MethodPost, "https://careme.test/kroger/order", strings.NewReader(form.Encode()))
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

func returnQuery(t *testing.T, response *httptest.ResponseRecorder) url.Values {
	t.Helper()
	require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
	destination, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/recipes", destination.Path)
	assert.Equal(t, "shopping-list-section", destination.Fragment)
	assert.Equal(t, "list-hash", destination.Query().Get("h"))
	return destination.Query()
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
	query := returnQuery(t, result)
	assert.Equal(t, "complete", query.Get("kroger_status"))
	assert.Equal(t, "1", query.Get("kroger_attempt"))
	assert.Equal(t, "2", query.Get("kroger_added"))
	assert.Equal(t, "2", query.Get("kroger_unmatched"))
	assert.Equal(t, "https://www.qfc.com/cart", query.Get("kroger_cart"))
	assert.Equal(t, "no-store", result.Header().Get("Cache-Control"))
	assert.Equal(t, "complete", returnQuery(t, postOrder(mux, form)).Get("kroger_status"))
	form.Set("items", `[{"id":"new","name":"New product"}]`)
	assert.Equal(t, "changed", returnQuery(t, postOrder(mux, form)).Get("kroger_status"))
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
			response := postOrder(mux, form)
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
			query := returnQuery(t, result)
			assert.Equal(t, "complete", query.Get("kroger_status"))
			assert.Equal(t, scenario.cart, query.Get("kroger_cart"))
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
			start := postOrder(mux, orderForm())
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
				assert.Equal(t, expected, returnQuery(t, result).Get("kroger_status"))
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
			if strings.HasPrefix(scenario, "legacy ") {
				status := strings.TrimPrefix(scenario, "legacy ")
				require.NoError(t, a.cache.Put(t.Context(), "kroger_cart_transfers/mock-clerk-user-id/list-hash", `{"status":"`+status+`","added":1,"sent":[{"id":"food","quantity":"1 cup"}]}`, cache.Unconditional()))
				expected = status
				if status == "complete" {
					expected = "changed"
				}
			}
			if scenario == "confirmation failed" {
				a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_cart_transfers/", confirmation: true}
				expected = "confirmation_failed"
			}
			mux := orderMux(a)
			assert.Equal(t, expected, returnQuery(t, postOrder(mux, orderForm())).Get("kroger_status"))
			repeat := returnQuery(t, postOrder(mux, orderForm())).Get("kroger_status")
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
	assert.Equal(t, "pending", returnQuery(t, second).Get("kroger_status"))
	assert.Equal(t, "complete", returnQuery(t, <-completed).Get("kroger_status"))
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
		{"attempt", "-1"},
		{"attempt", "0"},
		{"attempt", "../other"},
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
	request := httptest.NewRequest(http.MethodPost, "https://careme.test/kroger/order", strings.NewReader(orderForm().Encode()))
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
				assert.Equal(t, http.StatusInternalServerError, response.Code)
				return
			}
			assert.Equal(t, expected, returnQuery(t, response).Get("kroger_status"))
		})
	}
}

func TestKrogerAddAgainHasIndependentDuplicateProtection(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	a := orderFixture(t, func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 2 {
			close(entered)
			<-release
		}
		var payload struct {
			Items []CartItem `json:"items"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, []CartItem{{UPC: "food-upc", Quantity: 1}, {UPC: "wine-upc", Quantity: 1}}, payload.Items)
		return cartResponse(http.StatusNoContent, ""), nil
	})
	a.cache = cache.NewFileCache(t.TempDir())
	saveShopper(t, a, time.Now().Add(time.Hour))
	mux := orderMux(a)
	assert.Equal(t, "complete", returnQuery(t, postOrder(mux, orderForm())).Get("kroger_status"))
	form := orderForm()
	form.Set("attempt", "1")
	completed := make(chan *httptest.ResponseRecorder, 1)
	go func() { completed <- postOrder(mux, form) }()
	<-entered
	assert.Equal(t, "pending", returnQuery(t, postOrder(mux, form)).Get("kroger_status"))
	close(release)
	query := returnQuery(t, <-completed)
	assert.Equal(t, "complete", query.Get("kroger_status"))
	assert.Equal(t, "2", query.Get("kroger_attempt"))
	assert.Equal(t, "complete", returnQuery(t, postOrder(mux, form)).Get("kroger_status"))
	assert.Equal(t, "complete", returnQuery(t, postOrder(mux, orderForm())).Get("kroger_status"))
	assert.Equal(t, int32(2), calls.Load())
	form.Set("attempt", "2")
	assert.Equal(t, "complete", returnQuery(t, postOrder(mux, form)).Get("kroger_status"))
	assert.Equal(t, int32(3), calls.Load())
}

func TestKrogerAddAgainRequiresFinishedPriorTransfer(t *testing.T) {
	for _, status := range []string{"", "pending", "complete", "uncertain"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			a := orderFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				return cartResponse(http.StatusNoContent, ""), nil
			})
			saveShopper(t, a, time.Now().Add(time.Hour))
			if status != "" {
				require.NoError(t, a.saveJSON(t.Context(), transferKey("mock-clerk-user-id", "list-hash", 0), orderResult{Status: status}, cache.Unconditional()))
			}
			form := orderForm()
			form.Set("attempt", "1")
			query := returnQuery(t, postOrder(orderMux(a), form))
			if status == "complete" || status == "uncertain" {
				assert.Equal(t, "complete", query.Get("kroger_status"))
				assert.Equal(t, 1, calls)
			} else {
				assert.Zero(t, calls)
				assert.NotEqual(t, "complete", query.Get("kroger_status"))
			}
		})
	}
}
