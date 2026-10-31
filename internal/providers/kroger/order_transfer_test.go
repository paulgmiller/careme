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

func TestCartTransferFormPersistsResultAndRedirectsToList(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	input := ordertypes.TransferRequest{
		ReturnURL: "/recipes?h=list-hash#shopping-list-section",
		Location:  locationtypes.Location{ID: "70500874", Chain: "QFC", Name: "My QFC", Address: "123 Store Street"},
		Items:     []ai.ShoppingItem{{ProductID: "food", VariantID: "food-upc", Name: "Milk", Needed: "2 cups", Buy: new(float64(1))}},
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	form := url.Values{"idempotency_key": {"list-hash"}, "transfer_request": {string(body)}}
	mux := orderMux(a)
	response := postOrder(mux, form)
	transfer := transferResponse(t, a, response)
	assert.Equal(t, ordertypes.TransferApplied, transfer.Status)
	assert.Equal(t, []ordertypes.TransferItem{{ProductID: "food", Name: "Milk", AcceptedQuantity: 1}}, transfer.Items)
	var saved ordertypes.Transfer
	require.NoError(t, a.loadJSON(t.Context(), transferKey("mock-clerk-user-id", transfer.TransferID), &saved))
	assert.Equal(t, transfer, saved, "the record stores the shared result directly")
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
	connectOrder(t, mux, orderForm())
	exists, err := a.cache.Exists(t.Context(), transferKey("mock-clerk-user-id", "list-hash"))
	require.NoError(t, err)
	assert.False(t, exists)
	for _, path := range []string{"/kroger/order-transfers/list-hash", "/kroger/order-transfers/list-hash/connect"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusNotFound, response.Code)
	}
}

func TestCartTransferSaveFailureLeavesPending(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	a.cache = orderFailCache{Cache: a.cache, prefix: transferKey("mock-clerk-user-id", "list-hash"), confirmation: true}
	mux := orderMux(a)
	assert.Equal(t, http.StatusInternalServerError, postOrder(mux, orderForm()).Code)
	result := savedResult(t, a, "list-hash")
	assert.Equal(t, ordertypes.TransferPending, result.Status)
	assert.Empty(t, result.Items)
	assert.Equal(t, result, transferResponse(t, a, postOrder(mux, orderForm())))
	assert.Equal(t, 1, calls)
}

func TestCartTransferRejectsJSONBody(t *testing.T) {
	a := newTestOrderHandler(t, &CartClient{})
	body := `{"items":[{"id":"food"}],"location":{"id":"70500874","chain":"QFC"},"return_url":"/recipes"}`
	request := httptest.NewRequest(http.MethodPost, "/kroger/order-transfers", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	orderMux(a).ServeHTTP(response, request)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestCartTransferMissingUPCFailureIsSaved(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("missing UPC must prevent cart writes")
		return nil, nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	form := orderForm()
	setOrderField(t, form, "items", json.RawMessage(`[{"id":"missing","name":"Eggs"}]`))
	mux := orderMux(a)
	result := transferResponse(t, a, postOrder(mux, form))
	assert.Equal(t, ordertypes.TransferFailed, result.Status)
	assert.Empty(t, result.Items)
	assert.Equal(t, result, transferResponse(t, a, postOrder(mux, form)))
}

func TestCartTransferMissingProductIDRejectsWholeTransfer(t *testing.T) {
	for _, id := range []string{"", "  "} {
		a := orderFixture(t, func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid transfer must not write the cart")
			return nil, nil
		})
		saveShopper(t, a, time.Now().Add(time.Hour))
		a.client.HTTPClient = &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
			t.Fatal("invalid transfer must not contact Kroger")
			return nil, nil
		})}
		form := orderForm()
		setOrderField(t, form, "items", []ai.ShoppingItem{{ProductID: "food", Name: "Milk"}, {ProductID: id, Name: "Invalid item"}})
		response := postOrder(orderMux(a), form)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		var transfer ordertypes.Transfer
		assert.ErrorIs(t, a.loadJSON(t.Context(), transferKey("mock-clerk-user-id", "list-hash"), &transfer), cache.ErrNotFound)
	}
}

func TestCartTransferReservationFailureDoesNotWrite(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("must reserve transfer first"); return nil, nil })
	saveShopper(t, a, time.Now().Add(time.Hour))
	a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_order_transfers/"}
	assert.Equal(t, http.StatusConflict, postOrder(orderMux(a), orderForm()).Code)
	var result ordertypes.Transfer
	assert.ErrorIs(t, a.loadJSON(t.Context(), transferKey("mock-clerk-user-id", "list-hash"), &result), cache.ErrNotFound)
}

func TestCartTransferFailedLookupIsNotRetried(t *testing.T) {
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
	assert.Equal(t, ordertypes.TransferFailed, failure.Status)
	assert.Zero(t, calls)
	a.client.HTTPClient = client
	completed := transferResponse(t, a, postOrder(mux, orderForm()))
	assert.Equal(t, failure.TransferID, completed.TransferID)
	assert.Equal(t, failure, completed)
	assert.Zero(t, calls)
}

func TestOAuthCallbackReusesSavedOutcome(t *testing.T) {
	for _, tc := range []struct {
		status  int
		outcome ordertypes.TransferStatus
	}{
		{http.StatusNoContent, ordertypes.TransferApplied},
		{http.StatusServiceUnavailable, ordertypes.TransferFailed},
	} {
		t.Run(string(tc.outcome), func(t *testing.T) {
			calls := 0
			a := orderFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				return cartResponse(tc.status, ""), nil
			})
			mux := orderMux(a)
			start := connectOrder(t, mux, orderForm())
			cookie := start.Result().Cookies()[0]
			for _, query := range []string{"code=code", "error=access_denied", "code=replayed"} {
				callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?state="+cookie.Value+"&"+query, nil)
				callback.AddCookie(cookie)
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, callback)
				result := transferResponse(t, a, response)
				assert.Equal(t, tc.outcome, result.Status)
			}
			assert.Equal(t, 1, calls)
		})
	}
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
	callback.AddCookie(second)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, callback)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	exists, err := a.cache.Exists(t.Context(), transferKey("mock-clerk-user-id", "list-hash"))
	require.NoError(t, err)
	assert.False(t, exists)
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
	assert.Equal(t, ordertypes.TransferApplied, result.Status)
	assert.Equal(t, ordertypes.TransferItem{ProductID: "food", Name: "Milk", AcceptedQuantity: 2}, result.Items[0])
	assert.Equal(t, result, transferResponse(t, a, postOrder(mux, orderForm())))
	assert.Equal(t, 1, calls)
}

func TestCartTransferCallbackObservingPendingDoesNotResubmit(t *testing.T) {
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
	assert.Equal(t, ordertypes.TransferPending, pending.Status)
	assert.Equal(t, ordertypes.TransferApplied, transferResponse(t, a, <-completed).Status)
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
			exists, err := a.cache.Exists(t.Context(), transferKey("mock-clerk-user-id", key))
			require.NoError(t, err)
			assert.False(t, exists)
			cookie := start.Result().Cookies()[0]
			callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?state="+url.QueryEscape(cookie.Value)+"&code=code", nil)
			callback.AddCookie(cookie)
			completed := httptest.NewRecorder()
			mux.ServeHTTP(completed, callback)
			require.Equal(t, http.StatusSeeOther, completed.Code, completed.Body.String())
			assert.Equal(t, "/recipes?h=list-hash#shopping-list-section", completed.Header().Get("Location"))
			assert.Equal(t, ordertypes.TransferApplied, savedResult(t, a, key).Status)
			repeated := postOrder(mux, form)
			assert.Equal(t, http.StatusSeeOther, repeated.Code)
			assert.Equal(t, "/recipes?h=list-hash#shopping-list-section", repeated.Header().Get("Location"))
			assert.Equal(t, 1, calls)
			a.auth = transferUser{AuthClient: auth.DefaultMock(), id: "another-user"}
			another := connectOrder(t, mux, form)
			assert.NotEqual(t, "https://www.qfc.com/cart", another.Header().Get("Location"))
			assert.Equal(t, 1, calls)
		})
	}
}
