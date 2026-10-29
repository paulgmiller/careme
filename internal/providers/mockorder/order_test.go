package mockorder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransferPersistsAndReusesResult(t *testing.T) {
	c := cache.NewInMemoryCache()
	mux := http.NewServeMux()
	NewHandler(c, auth.DefaultMock()).Register(mux)
	input := ordertypes.OrderViewInput{UserID: "mock-clerk-user-id", ListKey: "list", Location: locationtypes.Location{ID: "any-provider"}, Items: []ai.ShoppingItem{{Name: "Milk"}, {Name: "Bread"}}, ReturnURL: "/recipes?h=list"}
	view, err := OrderView(t.Context(), c, input)
	require.NoError(t, err)
	form := url.Values{"idempotency_key": {view.IdempotencyKey}, "transfer_request": {view.TransferRequestJSON}}
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, transferURL, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	response := post()
	assert.Equal(t, http.StatusSeeOther, response.Code)
	assert.Equal(t, input.ReturnURL, response.Header().Get("Location"))
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	transfer, err := readTransfer(t.Context(), c, input.UserID, input.ListKey)
	require.NoError(t, err)
	assert.Equal(t, ordertypes.TransferApplied, transfer.Status)
	assert.Equal(t, []ordertypes.TransferItem{{Name: "Milk", AcceptedQuantity: 1}, {Name: "Bread", AcceptedQuantity: 1}}, transfer.Items)
	view, err = OrderView(t.Context(), c, input)
	require.NoError(t, err)
	assert.Equal(t, transfer.CartURL, view.ReviewCartURL)
	assert.Empty(t, view.TransferURL)
	// Changed items under the same key cannot replace the saved outcome.
	form.Set("transfer_request", `{"items":[{"name":"Rice"}],"return_url":"/recipes?h=list"}`)
	assert.Equal(t, http.StatusSeeOther, post().Code)
	repeated, err := readTransfer(t.Context(), c, input.UserID, input.ListKey)
	require.NoError(t, err)
	assert.Equal(t, transfer, repeated)
	cart := httptest.NewRecorder()
	mux.ServeHTTP(cart, httptest.NewRequest(http.MethodGet, view.ReviewCartURL, nil))
	require.Equal(t, http.StatusOK, cart.Code)
	var result ordertypes.Transfer
	require.NoError(t, json.Unmarshal(cart.Body.Bytes(), &result))
	assert.Equal(t, transfer, result)
	input.UserID = "another-viewer"
	view, err = OrderView(t.Context(), c, input)
	require.NoError(t, err)
	assert.Empty(t, view.ReviewCartURL)
	assert.NotEmpty(t, view.TransferURL)
}

func TestSavedTransferStatuses(t *testing.T) {
	for _, status := range []ordertypes.TransferStatus{ordertypes.TransferFailed, ordertypes.TransferApplied, ordertypes.TransferPending, ordertypes.TransferPendingLogin, "unknown", ""} {
		t.Run(string(status), func(t *testing.T) {
			c := cache.NewInMemoryCache()
			body, err := json.Marshal(ordertypes.Transfer{Status: status, CartURL: "/mock/cart"})
			require.NoError(t, err)
			require.NoError(t, c.Put(t.Context(), transferKey("viewer", "list"), string(body), cache.Unconditional()))
			view, err := OrderView(t.Context(), c, ordertypes.OrderViewInput{UserID: "viewer", ListKey: "list"})
			if status == "unknown" || status == "" {
				require.ErrorContains(t, err, "unknown mock transfer status")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, status == ordertypes.TransferFailed, view.TransferFailed)
			assert.Empty(t, view.TransferURL)
			if !view.TransferFailed {
				assert.Equal(t, "/mock/cart", view.ReviewCartURL)
			}
		})
	}
}

type brokenCache struct{ cache.Cache }

func (brokenCache) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("cache unavailable")
}

func TestTransferReadErrors(t *testing.T) {
	for _, c := range []cache.Cache{brokenCache{cache.NewInMemoryCache()}, cache.NewInMemoryCache()} {
		require.NoError(t, c.Put(t.Context(), transferKey("mock-clerk-user-id", "list"), "broken json", cache.Unconditional()))
		view, err := OrderView(t.Context(), c, ordertypes.OrderViewInput{UserID: "mock-clerk-user-id", ListKey: "list"})
		require.Error(t, err)
		assert.Nil(t, view)
		mux := http.NewServeMux()
		NewHandler(c, auth.DefaultMock()).Register(mux)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, transferURL+"/list", nil))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	}
}

func TestTransferRejectsInvalidForm(t *testing.T) {
	for _, body := range []string{
		"transfer_request=%",
		"transfer_request=invalid&idempotency_key=list",
		`transfer_request={"items":[{"name":"Milk"}],"return_url":"https://elsewhere.test"}&idempotency_key=list`,
		`transfer_request={"items":[{"name":"Milk"}],"return_url":"/recipes"}&idempotency_key=../other`,
		`transfer_request={"items":[],"return_url":"/recipes"}&idempotency_key=list`,
	} {
		t.Run(body, func(t *testing.T) {
			c := cache.NewInMemoryCache()
			mux := http.NewServeMux()
			NewHandler(c, auth.DefaultMock()).Register(mux)
			r := httptest.NewRequest(http.MethodPost, transferURL, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			assert.Equal(t, http.StatusBadRequest, w.Code)
			_, err := readTransfer(t.Context(), c, "mock-clerk-user-id", "list")
			assert.ErrorIs(t, err, cache.ErrNotFound)
		})
	}
}

type writeErrorCache struct{ cache.Cache }

func (writeErrorCache) Put(context.Context, string, string, cache.PutOptions) error {
	return errors.New("cache unavailable")
}

func TestTransferStorageFailureDoesNotSucceed(t *testing.T) {
	for _, c := range []cache.Cache{brokenCache{cache.NewInMemoryCache()}, writeErrorCache{cache.NewInMemoryCache()}} {
		mux := http.NewServeMux()
		NewHandler(c, auth.DefaultMock()).Register(mux)
		form := url.Values{"idempotency_key": {"list"}, "transfer_request": {`{"items":[{"name":"Milk"}],"return_url":"/recipes"}`}}
		r := httptest.NewRequest(http.MethodPost, transferURL, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Empty(t, w.Header().Get("Location"))
	}
}
