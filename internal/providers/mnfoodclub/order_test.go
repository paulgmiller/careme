package mnfoodclub

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

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOrderTest(t *testing.T, api http.HandlerFunc) (*OrderHandler, *OrderClient, cache.Cache) {
	t.Helper()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client, err := NewOrderClient(config.MNFoodClubConfig{OrderAPIURL: server.URL, OrderAccessToken: "partner-only-token"}, server.Client())
	require.NoError(t, err)
	c := cache.NewInMemoryCache()
	return NewOrderHandler(c, auth.DefaultMock(), client), client, c
}

func orderInput() ordertypes.TransferRequest {
	return ordertypes.TransferRequest{
		Location: locationtypes.Location{ID: TCFarmLocationID, Chain: "TC Farm"}, ReturnURL: "/recipes?h=list",
		Items:      []ai.ShoppingItem{{ProductID: "101", Name: "Chicken", Buy: new(float64(3))}, {ProductID: "202", Name: "Eggs"}, {Name: "Salt", Needed: "1 tsp"}},
		VariantIDs: map[string]string{"101": "1005"},
	}
}

func postTransfer(t *testing.T, h *OrderHandler, input ordertypes.TransferRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	require.NoError(t, err)
	form := url.Values{"idempotency_key": {"list"}, "transfer_request": {string(body)}}
	r := httptest.NewRequest(http.MethodPost, orderTransferURL, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux := http.NewServeMux()
	h.Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

const (
	appliedResult = `{"transfer_id":"partner-1","status":"applied","cart_url":"https://shop.foodclub.test/cart","user_id":"customer-42","items":[{"product_id":"101","accepted_quantity":2,"rejected_quantity":1,"rejection_reason":"insufficient_stock"},{"product_id":"202","accepted_quantity":0,"rejected_quantity":1,"rejection_reason":"new-partner-reason"}]}`
	pendingResult = `{"transfer_id":"partner-1","status":"pending_login","cart_url":"https://shop.foodclub.test/handoff/opaque"}`
)

func TestOrderAppliedRelaysFormAndReusesResult(t *testing.T) {
	var calls atomic.Int32
	h, client, c := newOrderTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/cart-transfers", r.URL.Path)
		assert.Equal(t, "Bearer partner-only-token", r.Header.Get("Authorization"))
		assert.Len(t, r.Header.Get("Idempotency-Key"), 64)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var request handoffRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, []requestedItem{{ProductID: "101", VariantID: "1005", Quantity: 3}, {ProductID: "202", Quantity: 1}}, request.Items)
		assert.Equal(t, []string{"you@careme.cooking"}, request.IdentityHints.Emails)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(appliedResult))
	})
	response := postTransfer(t, h, orderInput())
	require.Equal(t, http.StatusSeeOther, response.Code, response.Body.String())
	assert.Equal(t, "/recipes?h=list", response.Header().Get("Location"))
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	transfer, err := loadOrderTransfer(t.Context(), c, "mock-clerk-user-id", "list")
	require.NoError(t, err)
	assert.Equal(t, ordertypes.Transfer{TransferID: "partner-1", Status: ordertypes.TransferApplied, CartURL: "https://shop.foodclub.test/cart", Added: 2, Unmatched: 3}, transfer)
	input := orderInput()
	input.Items = []ai.ShoppingItem{{ProductID: "different"}}
	input.ReturnURL = "/recipes?h=other"
	assert.Equal(t, input.ReturnURL, postTransfer(t, h, input).Header().Get("Location"))
	view, err := OrderViewForUser(t.Context(), c, client, ordertypes.OrderViewInput{UserID: "mock-clerk-user-id", ListKey: "list"})
	require.NoError(t, err)
	assert.Equal(t, transfer.CartURL, view.ReviewCartURL)
	assert.Empty(t, view.TransferURL)
	assert.Equal(t, int32(1), calls.Load())
	view, err = OrderViewForUser(t.Context(), c, client, ordertypes.OrderViewInput{UserID: "other-viewer", ListKey: "list", Location: orderInput().Location})
	require.NoError(t, err)
	assert.Equal(t, orderTransferURL, view.TransferURL)
	assert.Empty(t, view.ReviewCartURL)
}

func TestOrderPendingLoginRedirectsAndRefreshesOnServer(t *testing.T) {
	var posts, gets atomic.Int32
	var loggedIn atomic.Bool
	h, client, c := newOrderTest(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer partner-only-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts.Add(1)
			_, _ = w.Write([]byte(pendingResult))
			return
		}
		gets.Add(1)
		assert.Equal(t, "/v1/cart-transfers/partner-1", r.URL.Path)
		assert.Empty(t, r.Header.Get("Idempotency-Key"))
		if loggedIn.Load() {
			_, _ = w.Write([]byte(appliedResult))
		} else {
			_, _ = w.Write([]byte(pendingResult))
		}
	})
	for range 2 {
		response := postTransfer(t, h, orderInput())
		assert.Equal(t, http.StatusSeeOther, response.Code)
		assert.Equal(t, "https://shop.foodclub.test/handoff/opaque", response.Header().Get("Location"))
	}
	input := ordertypes.OrderViewInput{UserID: "mock-clerk-user-id", ListKey: "list"}
	view, err := OrderViewForUser(t.Context(), c, client, input)
	require.NoError(t, err)
	assert.Equal(t, "https://shop.foodclub.test/handoff/opaque", view.ReviewCartURL)
	loggedIn.Store(true)
	view, err = OrderViewForUser(t.Context(), c, client, input)
	require.NoError(t, err)
	assert.Equal(t, "https://shop.foodclub.test/cart", view.ReviewCartURL)
	transfer, err := loadOrderTransfer(t.Context(), c, input.UserID, input.ListKey)
	require.NoError(t, err)
	assert.Equal(t, 2, transfer.Added)
	assert.Equal(t, 3, transfer.Unmatched)
	_, err = OrderViewForUser(t.Context(), c, client, input)
	require.NoError(t, err)
	assert.Equal(t, "/recipes?h=list", postTransfer(t, h, orderInput()).Header().Get("Location"))
	assert.Equal(t, int32(1), posts.Load())
	assert.Equal(t, int32(2), gets.Load(), "applied transfers no longer need status reads")
}

func TestOrderOperationalFailuresAreSavedWithoutRetries(t *testing.T) {
	for _, status := range []int{400, 401, 403, 409, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			h, client, c := newOrderTest(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(status)
			})
			for range 2 {
				response := postTransfer(t, h, orderInput())
				assert.Equal(t, http.StatusSeeOther, response.Code)
				assert.Equal(t, "/recipes?h=list", response.Header().Get("Location"))
			}
			view, err := OrderViewForUser(t.Context(), c, client, ordertypes.OrderViewInput{UserID: "mock-clerk-user-id", ListKey: "list"})
			require.NoError(t, err)
			assert.True(t, view.TransferFailed)
			assert.Empty(t, view.TransferURL)
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestOrderRejectsInvalidPartnerResponses(t *testing.T) {
	for _, response := range []string{
		`not json`, `{}`,
		strings.Replace(appliedResult, `"status":"applied"`, `"status":"unknown"`, 1),
		strings.Replace(appliedResult, `https://shop.foodclub.test/cart`, `javascript:alert(1)`, 1),
		strings.Replace(appliedResult, `"accepted_quantity":2`, `"accepted_quantity":1`, 1),
		strings.Replace(appliedResult, `"product_id":"202"`, `"product_id":"101"`, 1),
		strings.Replace(appliedResult, `"user_id":"customer-42"`, `"user_id":""`, 1),
		strings.Replace(appliedResult, `"rejection_reason":"insufficient_stock"`, `"rejection_reason":""`, 1),
	} {
		t.Run(response, func(t *testing.T) {
			h, _, c := newOrderTest(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response))
			})
			assert.Equal(t, http.StatusSeeOther, postTransfer(t, h, orderInput()).Code)
			transfer, err := loadOrderTransfer(t.Context(), c, "mock-clerk-user-id", "list")
			require.NoError(t, err)
			assert.Equal(t, ordertypes.TransferFailed, transfer.Status)
		})
	}
}

func TestOrderItemsUsePackagesAndUniqueProducts(t *testing.T) {
	input := orderInput()
	input.Items = append(input.Items, ai.ShoppingItem{ProductID: "101", Name: "More chicken", Buy: new(float64(0.5))})
	request, unmatched, err := orderItems(input)
	require.NoError(t, err)
	assert.Equal(t, []requestedItem{{ProductID: "101", VariantID: "1005", Quantity: 4}, {ProductID: "202", Quantity: 1}}, request.Items)
	assert.Equal(t, 1, unmatched)
	input.Items[0].Buy = new(float64(-1))
	_, _, err = orderItems(input)
	require.Error(t, err)
}

func TestOrderNoProductIDsFailsWithoutPartnerRequest(t *testing.T) {
	h, _, c := newOrderTest(t, func(w http.ResponseWriter, r *http.Request) { t.Error("must not call partner without product IDs") })
	input := orderInput()
	input.Items = []ai.ShoppingItem{{Name: "Salt"}}
	assert.Equal(t, http.StatusSeeOther, postTransfer(t, h, input).Code)
	transfer, err := loadOrderTransfer(t.Context(), c, "mock-clerk-user-id", "list")
	require.NoError(t, err)
	assert.Equal(t, ordertypes.TransferFailed, transfer.Status)
	assert.Equal(t, 1, transfer.Unmatched)
}

func TestOrderPendingStatusFailureIsHardError(t *testing.T) {
	h, client, c := newOrderTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pendingResult))
	})
	postTransfer(t, h, orderInput())
	view, err := OrderViewForUser(t.Context(), c, client, ordertypes.OrderViewInput{UserID: "mock-clerk-user-id", ListKey: "list"})
	require.ErrorContains(t, err, "404")
	assert.Nil(t, view)
	transfer, err := loadOrderTransfer(t.Context(), c, "mock-clerk-user-id", "list")
	require.NoError(t, err)
	assert.Equal(t, ordertypes.TransferPending, transfer.Status)
}

type orderUser struct {
	auth.AuthClient
	id string
}

func (u orderUser) GetUserIDFromRequest(*http.Request) (string, error) { return u.id, nil }

func TestOrderIdempotencyIsScopedToCaremeUser(t *testing.T) {
	keys := make(chan string, 2)
	h, _, _ := newOrderTest(t, func(w http.ResponseWriter, r *http.Request) {
		keys <- r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(appliedResult))
	})
	for _, id := range []string{"first-user", "second-user"} {
		h.auth = orderUser{AuthClient: auth.DefaultMock(), id: id}
		assert.Equal(t, http.StatusSeeOther, postTransfer(t, h, orderInput()).Code)
	}
	assert.NotEqual(t, <-keys, <-keys, "different shoppers must not share an integration-wide idempotency key")
}

func TestOrderAllRejectedIsApplied(t *testing.T) {
	h, _, c := newOrderTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Replace(strings.Replace(appliedResult, `"accepted_quantity":2`, `"accepted_quantity":0`, 1), `"rejected_quantity":1`, `"rejected_quantity":3`, 1)))
	})
	assert.Equal(t, http.StatusSeeOther, postTransfer(t, h, orderInput()).Code)
	transfer, err := loadOrderTransfer(t.Context(), c, "mock-clerk-user-id", "list")
	require.NoError(t, err)
	assert.Equal(t, ordertypes.TransferApplied, transfer.Status)
	assert.Zero(t, transfer.Added)
	assert.Equal(t, 3, transfer.Unmatched)
}

type orderWriteFailure struct {
	cache.Cache
	finalOnly bool
}

func (c orderWriteFailure) Put(ctx context.Context, key, value string, options cache.PutOptions) error {
	if !c.finalOnly || options.Condition == cache.PutUnconditional {
		return errors.New("cache unavailable")
	}
	return c.Cache.Put(ctx, key, value, options)
}

func TestOrderStorageFailuresDoNotResubmit(t *testing.T) {
	for _, finalOnly := range []bool{false, true} {
		var calls atomic.Int32
		h, _, c := newOrderTest(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(appliedResult))
		})
		h.cache = orderWriteFailure{Cache: c, finalOnly: finalOnly}
		assert.Equal(t, http.StatusInternalServerError, postTransfer(t, h, orderInput()).Code)
		if finalOnly {
			assert.Equal(t, http.StatusInternalServerError, postTransfer(t, h, orderInput()).Code)
			assert.Equal(t, int32(1), calls.Load())
		} else {
			assert.Zero(t, calls.Load(), "the reservation must be saved before submitting")
		}
	}
}

func TestOrderClientPreservesTransportWithoutReplayingFailures(t *testing.T) {
	var calls atomic.Int32
	client, err := NewOrderClient(config.MNFoodClubConfig{OrderAPIURL: "https://partner.foodclub.test", OrderAccessToken: "partner-token"}, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		assert.Equal(t, "partner.foodclub.test", r.URL.Host)
		return nil, errors.New("connection lost")
	})})
	require.NoError(t, err)
	_, err = client.create(t.Context(), "original-key", handoffRequest{Items: []requestedItem{{ProductID: "101", Quantity: 1}}})
	require.Error(t, err)
	assert.Equal(t, int32(1), calls.Load())
}
