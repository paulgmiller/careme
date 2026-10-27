package kroger

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCartTransferFormPersistsRequestAndRedirectsToCart(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	input := ordertypes.TransferRequest{
		ReturnURL: "/recipes?h=list-hash#shopping-list-section",
		Location:  locationtypes.Location{ID: "70500874", Chain: "QFC", Name: "My QFC", Address: "123 Store Street"},
		Items:     []ai.ShoppingItem{{ProductID: "food", Name: "Milk", Needed: "2 cups"}},
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	form := url.Values{"idempotency_key": {"list-hash"}, "transfer_request": {string(body)}}
	mux := orderMux(a)
	response := postOrder(mux, form)
	transfer := transferResponse(t, a, response)
	assert.Equal(t, "applied", transfer.Status)
	assert.Equal(t, 1, transfer.Added)
	var saved cartTransfer
	require.NoError(t, a.loadJSON(t.Context(), transferKey("mock-clerk-user-id", transfer.TransferID)+"/state", &saved))
	assert.Equal(t, input, saved.Request, "the full shared request survives the transfer handoff")
	assert.Equal(t, transfer, saved.Result, "the saved result uses the same contract as the API")
	var submission ordertypes.Transfer
	require.NoError(t, a.loadJSON(t.Context(), submissionKey(saved), &submission))
	assert.Equal(t, transfer, submission)
	assert.Equal(t, "mock-clerk-user-id", saved.UserID)
	assert.Equal(t, "list-hash", saved.IdempotencyKey)
	for range 3 {
		assert.Equal(t, transfer, transferResponse(t, a, postOrder(mux, form)))
	}
	assert.Equal(t, 1, calls)
	a.auth = orderNoSession{AuthClient: auth.DefaultMock()}
	assert.Equal(t, http.StatusSeeOther, postOrder(mux, form).Code)
}

type transferUser struct {
	auth.AuthClient
	id string
}

func (a transferUser) GetUserIDFromRequest(*http.Request) (string, error) { return a.id, nil }

func TestCartTransferPendingLoginRedirectsDirectlyToOAuth(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("GET must not write cart"); return nil, nil })
	mux := orderMux(a)
	posted := transferResponse(t, a, postOrder(mux, orderForm()))
	assert.Equal(t, "pending_login", posted.Status)
	assert.Equal(t, "https://www.qfc.com/cart", posted.CartURL)
	assert.Equal(t, posted, transferResponse(t, a, postOrder(mux, orderForm())))
	assert.Zero(t, posted.Added)
	for _, path := range []string{"/kroger/cart-transfers/list-hash", "/kroger/cart-transfers/list-hash/connect"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusNotFound, response.Code)
	}
}

func TestCartTransferLifecycleSaveFailureKeepsSubmissionResult(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	a.cache = orderFailCache{Cache: a.cache, prefix: transferKey("mock-clerk-user-id", "list-hash") + "/state", confirmation: true}
	mux := orderMux(a)
	assert.Equal(t, http.StatusInternalServerError, postOrder(mux, orderForm()).Code)
	result := savedResult(t, a, "list-hash")
	assert.Equal(t, "applied", result.Status)
	assert.Equal(t, 2, result.Added)
	assert.Equal(t, result, transferResponse(t, a, postOrder(mux, orderForm())))
	assert.Equal(t, 1, calls)
}

func TestCartTransferRejectsJSONBody(t *testing.T) {
	a := newTestOrderHandler(t, &CartClient{})
	body := `{"items":[{"id":"food"}],"location":{"id":"70500874","chain":"QFC"},"return_url":"/recipes"}`
	request := httptest.NewRequest(http.MethodPost, "/kroger/cart-transfers", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	orderMux(a).ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestCartTransferNoMatchesIsSaved(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("no matching products"); return nil, nil })
	saveShopper(t, a, time.Now().Add(time.Hour))
	form := orderForm()
	setOrderField(t, form, "items", json.RawMessage(`[{"name":"Salt"},{"id":"missing","name":"Eggs"}]`))
	mux := orderMux(a)
	result := transferResponse(t, a, postOrder(mux, form))
	assert.Equal(t, "applied", result.Status)
	assert.Zero(t, result.Added)
	assert.Equal(t, 2, result.Unmatched)
	assert.Equal(t, result, transferResponse(t, a, postOrder(mux, form)))
}

func TestCartTransferReservationFailureDoesNotWrite(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("must reserve transfer first"); return nil, nil })
	saveShopper(t, a, time.Now().Add(time.Hour))
	a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_order_transfers/"}
	assert.Equal(t, http.StatusInternalServerError, postOrder(orderMux(a), orderForm()).Code)
	var result ordertypes.Transfer
	assert.ErrorIs(t, a.loadJSON(t.Context(), transferKey("mock-clerk-user-id", "list-hash")+"/submission", &result), cache.ErrNotFound)
}

func TestCartTransferRetriesProductLookupWithSameID(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	client := a.client.HTTPClient
	a.client.HTTPClient = &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
		return cartResponse(http.StatusServiceUnavailable, ""), nil
	})}
	mux := orderMux(a)
	failure := transferResponse(t, a, postOrder(mux, orderForm()))
	assert.Equal(t, "failed", failure.Status)
	assert.Zero(t, calls)
	a.client.HTTPClient = client
	completed := transferResponse(t, a, postOrder(mux, orderForm()))
	assert.Equal(t, failure.TransferID, completed.TransferID)
	assert.Equal(t, "applied", completed.Status)
	assert.Equal(t, 1, calls)
}

func TestLateOAuthDenialDoesNotHideCompletedCartTransfer(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	mux := orderMux(a)
	start := connectOrder(t, mux, orderForm())
	cookie := start.Result().Cookies()[0]
	for _, outcome := range []string{"code=code", "error=access_denied", "code=replayed"} {
		callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?state="+cookie.Value+"&"+outcome, nil)
		callback.AddCookie(cookie)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, callback)
		result := transferResponse(t, a, response)
		assert.Equal(t, "applied", result.Status)
		assert.Equal(t, 2, result.Added)
	}
	assert.Equal(t, 1, calls)
}

func TestCartTransferNewConnectionReplacesOAuthState(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("replaced OAuth state must not write cart")
		return nil, nil
	})
	mux := orderMux(a)
	first := connectOrder(t, mux, orderForm()).Result().Cookies()[0]
	second := connectOrder(t, mux, orderForm()).Result().Cookies()[0]
	require.NotEqual(t, first.Value, second.Value)
	callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?state="+first.Value+"&code=code", nil)
	callback.AddCookie(first)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, callback)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, "pending_login", savedResult(t, a, "list-hash").Status)
}

func TestCartTransferFileCacheOAuthAndDuplicateProtection(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	a.cache = cache.NewFileCache(t.TempDir())
	mux := orderMux(a)
	start := connectOrder(t, mux, orderForm())
	cookie := start.Result().Cookies()[0]
	callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?state="+cookie.Value+"&code=code", nil)
	callback.AddCookie(cookie)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, callback)
	result := transferResponse(t, a, response)
	assert.Equal(t, "applied", result.Status)
	assert.Equal(t, 2, result.Added)
	assert.Equal(t, result, transferResponse(t, a, postOrder(mux, orderForm())))
	assert.Equal(t, 1, calls)
}

func TestCartTransferConcurrentOAuthCallbacksWriteOnce(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		close(entered)
		<-release
		return cartResponse(http.StatusNoContent, ""), nil
	})
	mux := orderMux(a)
	start := connectOrder(t, mux, orderForm())
	cookie := start.Result().Cookies()[0]
	callback := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/kroger/callback?state="+cookie.Value+"&code=code", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		return response
	}
	completed := make(chan *httptest.ResponseRecorder, 1)
	go func() { completed <- callback() }()
	<-entered
	second := callback()
	pending := transferResponse(t, a, second)
	close(release)
	assert.Equal(t, "uncertain", pending.Status)
	assert.Equal(t, "applied", transferResponse(t, a, <-completed).Status)
	assert.Equal(t, int32(1), calls.Load())
}

func TestCartTransferUsesIdempotencyKeyThroughOAuth(t *testing.T) {
	for _, key := range []string{"list-hash", "list.with.dots", "list?query#fragment%+&="} {
		t.Run(key, func(t *testing.T) {
			calls := 0
			a := orderFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				return cartResponse(http.StatusNoContent, ""), nil
			})
			a.cache = cache.NewFileCache(t.TempDir())
			mux := orderMux(a)
			form := orderForm()
			form.Set("idempotency_key", key)
			start := connectOrder(t, mux, form)
			result := savedResult(t, a, key)
			assert.Equal(t, key, result.TransferID)
			cookie := start.Result().Cookies()[0]
			callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?state="+url.QueryEscape(cookie.Value)+"&code=code", nil)
			callback.AddCookie(cookie)
			completed := httptest.NewRecorder()
			mux.ServeHTTP(completed, callback)
			require.Equal(t, http.StatusSeeOther, completed.Code, completed.Body.String())
			assert.Equal(t, "https://www.qfc.com/cart", completed.Header().Get("Location"))
			assert.Equal(t, "applied", savedResult(t, a, key).Status)
			repeated := postOrder(mux, form)
			assert.Equal(t, http.StatusSeeOther, repeated.Code)
			assert.Equal(t, "https://www.qfc.com/cart", repeated.Header().Get("Location"))
			assert.Equal(t, 1, calls)
			a.auth = transferUser{AuthClient: auth.DefaultMock(), id: "another-user"}
			another := connectOrder(t, mux, form)
			assert.NotEqual(t, "https://www.qfc.com/cart", another.Header().Get("Location"))
			assert.Equal(t, 1, calls)
		})
	}
}
