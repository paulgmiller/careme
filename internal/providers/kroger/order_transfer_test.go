package kroger

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCartTransferJSONAndReadOnlyStatus(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	input := ordertypes.TransferRequest{
		ListKey: "list-hash", ReturnURL: "/recipes?h=list-hash#shopping-list-section",
		Location: locationtypes.Location{ID: "70500874", Chain: "QFC"},
		Items:    []ai.ShoppingItem{{ProductID: "food", Name: "Milk", Needed: "2 cups"}},
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/kroger/cart-transfers", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	mux := orderMux(a)
	assert.Equal(t, http.StatusNotFound, getTransfer(mux, ordertypes.TransferID(input.ListKey)).Code)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	transfer := transferResponse(t, response)
	assert.Equal(t, "applied", transfer.Status)
	assert.Equal(t, 1, transfer.Added)
	for range 3 {
		assert.Equal(t, transfer, transferResponse(t, getTransfer(mux, transfer.TransferID)))
	}
	assert.Equal(t, 1, calls)
	a.auth = transferUser{AuthClient: auth.DefaultMock(), id: "another-user"}
	assert.Equal(t, http.StatusNotFound, getTransfer(mux, transfer.TransferID).Code)
	a.auth = orderNoSession{AuthClient: auth.DefaultMock()}
	assert.Equal(t, http.StatusUnauthorized, getTransfer(mux, transfer.TransferID).Code)
}

type transferUser struct {
	auth.AuthClient
	id string
}

func (a transferUser) GetUserIDFromRequest(*http.Request) (string, error) { return a.id, nil }

func TestCartTransferPendingLoginCanBeQueried(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("GET must not write cart"); return nil, nil })
	mux := orderMux(a)
	posted := transferResponse(t, postOrder(mux, orderForm()))
	assert.Equal(t, "pending_login", posted.Status)
	assert.Equal(t, "/kroger/cart-transfers/"+posted.TransferID+"/connect", posted.CartURL)
	assert.Equal(t, posted, transferResponse(t, getTransfer(mux, posted.TransferID)))
	assert.Equal(t, posted, transferResponse(t, postOrder(mux, orderForm())))
	assert.Zero(t, posted.Added)
	for _, id := range []string{"!invalid", ordertypes.TransferID("../private"), ordertypes.TransferID("unknown")} {
		assert.Equal(t, http.StatusNotFound, getTransfer(mux, id).Code)
	}
}

func TestCartTransferLifecycleSaveFailureKeepsCartResult(t *testing.T) {
	calls := 0
	a := orderFixture(t, func(*http.Request) (*http.Response, error) {
		calls++
		return cartResponse(http.StatusNoContent, ""), nil
	})
	saveShopper(t, a, time.Now().Add(time.Hour))
	a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_order_transfers/", confirmation: true}
	mux := orderMux(a)
	assert.Equal(t, http.StatusInternalServerError, postOrder(mux, orderForm()).Code)
	result := transferResponse(t, getTransfer(mux, ordertypes.TransferID("list-hash")))
	assert.Equal(t, "applied", result.Status)
	assert.Equal(t, 2, result.Added)
	assert.Equal(t, result, transferResponse(t, postOrder(mux, orderForm())))
	assert.Equal(t, 1, calls)
}

func TestCartTransferJSONValidation(t *testing.T) {
	a := newTestOrderHandler(t, &CartClient{})
	mux := orderMux(a)
	for _, body := range []string{`{`, `null`, `{}`, `{"list_key":"list","user_id":"someone-else"}`, `{} {}`} {
		request := httptest.NewRequest(http.MethodPost, "/kroger/cart-transfers", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
	}
}

func TestCartTransferNoMatchesIsSaved(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("no matching products"); return nil, nil })
	saveShopper(t, a, time.Now().Add(time.Hour))
	form := orderForm()
	form.Set("items", `[{"name":"Salt"},{"id":"missing","name":"Eggs"}]`)
	mux := orderMux(a)
	result := transferResponse(t, postOrder(mux, form))
	assert.Equal(t, "applied", result.Status)
	assert.Zero(t, result.Added)
	assert.Equal(t, 2, result.Unmatched)
	assert.Equal(t, result, transferResponse(t, getTransfer(mux, result.TransferID)))
}

func TestCartTransferReservationFailureDoesNotWrite(t *testing.T) {
	a := orderFixture(t, func(*http.Request) (*http.Response, error) { t.Fatal("must reserve transfer first"); return nil, nil })
	saveShopper(t, a, time.Now().Add(time.Hour))
	a.cache = orderFailCache{Cache: a.cache, prefix: "kroger_order_transfers/"}
	assert.Equal(t, http.StatusInternalServerError, postOrder(orderMux(a), orderForm()).Code)
	var result orderResult
	assert.ErrorIs(t, a.loadJSON(t.Context(), "kroger_cart_transfers/mock-clerk-user-id/list-hash", &result), cache.ErrNotFound)
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
	failure := transferResponse(t, postOrder(mux, orderForm()))
	assert.Equal(t, "failed", failure.Status)
	assert.Zero(t, calls)
	a.client.HTTPClient = client
	completed := transferResponse(t, postOrder(mux, orderForm()))
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
		result := callbackTransfer(t, mux, response)
		assert.Equal(t, "applied", result.Status)
		assert.Equal(t, 2, result.Added)
	}
	assert.Equal(t, 1, calls)
}
