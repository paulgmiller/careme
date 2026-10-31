package instacart

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
	"careme/internal/templates"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testHandler(t *testing.T, store cache.Cache, transport http.RoundTripper) *OrderHandler {
	t.Helper()
	client, err := NewOrderClient(config.InstacartConfig{APIKey: "key"}, "https://careme.test", &http.Client{Transport: transport})
	require.NoError(t, err)
	return NewOrderHandler(store, auth.DefaultMock(), client)
}

func orderInput() ordertypes.TransferRequest {
	buy := 7.0
	return ordertypes.TransferRequest{
		Items:    []ai.ShoppingItem{{ProductID: "source-product", VariantID: "source-variant", Name: `Chicken & "herbs"`, Needed: "2 lbs", Buy: &buy}, {Name: "Olive oil", Needed: "4½ tbsp"}, {Name: "Wine", Needed: "1 bottle"}},
		Location: locationtypes.Location{ID: "any-store", Name: "Any store"}, ReturnURL: "/recipes?h=list-hash#shopping-list-section",
	}
}

func orderForm(t *testing.T, input ordertypes.TransferRequest, key string) url.Values {
	t.Helper()
	body, err := json.Marshal(input)
	require.NoError(t, err)
	return url.Values{"transfer_request": {string(body)}, "idempotency_key": {key}}
}

func submit(handler *OrderHandler, form url.Values) *httptest.ResponseRecorder {
	routes := http.NewServeMux()
	handler.Register(routes)
	r := httptest.NewRequest(http.MethodPost, "https://careme.test"+orderTransferURL, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, r)
	return w
}

func TestOrderPayloadAndSavedTransfer(t *testing.T) {
	calls := 0
	store := cache.NewInMemoryCache()
	handler := testHandler(t, store, transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.NotContains(t, string(body), "source-product")
		assert.NotContains(t, string(body), "source-variant")
		var payload shoppingList
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.Equal(t, "Careme shopping list", payload.Title)
		assert.Equal(t, "shopping_list", payload.LinkType)
		assert.Equal(t, 30, payload.ExpiresIn)
		assert.Equal(t, "https://careme.test/recipes?h=list-hash#shopping-list-section", payload.LandingPage.LinkbackURL)
		require.Len(t, payload.Items, 3)
		assert.Equal(t, lineItem{Name: `Chicken & "herbs"`, DisplayText: `Chicken & "herbs" — 2 lbs`, Measurements: []measurement{{Quantity: 2, Unit: "pound"}}}, payload.Items[0])
		assert.Equal(t, []measurement{{Quantity: 4.5, Unit: "tablespoon"}}, payload.Items[1].Measurements)
		assert.Equal(t, lineItem{Name: "Wine", DisplayText: "Wine — 1 bottle"}, payload.Items[2])
		return response(200, `{"products_link_url":"https://www.instacart.com/store/shopping_lists/1"}`), nil
	}))
	input := orderInput()
	for range 2 {
		w := submit(handler, orderForm(t, input, "list-hash"))
		assert.Equal(t, 303, w.Code)
		assert.Equal(t, input.ReturnURL, w.Header().Get("Location"))
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
	transfer, err := loadOrderTransfer(t.Context(), store, "mock-clerk-user-id", "list-hash")
	require.NoError(t, err)
	assert.Equal(t, ordertypes.Transfer{TransferID: "list-hash", Status: ordertypes.TransferApplied, CartURL: "https://www.instacart.com/store/shopping_lists/1"}, transfer)
	assert.Empty(t, transfer.Items, "creating a shopping page does not confirm cart acceptance")
	// The finalized key identifies the intended handoff; do not compare changed payloads.
	input.Items[0].Needed = "4 lbs"
	assert.Equal(t, 303, submit(handler, orderForm(t, input, "list-hash")).Code)
	assert.Equal(t, 1, calls)
	// A different list and a different authenticated shopper each get their own record.
	assert.Equal(t, 303, submit(handler, orderForm(t, orderInput(), "another-list")).Code)
	handler.auth = requestAuth{AuthClient: auth.DefaultMock(), userID: "second-user"}
	assert.Equal(t, 303, submit(handler, orderForm(t, orderInput(), "list-hash")).Code)
	assert.Equal(t, 3, calls)
}

type requestAuth struct {
	auth.AuthClient
	userID string
	err    error
}

func (a requestAuth) GetUserIDFromRequest(_ *http.Request) (string, error) { return a.userID, a.err }

type failingCache struct {
	cache.Cache
	readErr, writeErr error
	confirmation      bool
	body              string
}

func (c failingCache) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if c.readErr != nil {
		return nil, c.readErr
	}
	if c.body != "" {
		return io.NopCloser(strings.NewReader(c.body)), nil
	}
	return c.Cache.Get(ctx, key)
}

func (c failingCache) Put(ctx context.Context, key, value string, options cache.PutOptions) error {
	if c.writeErr != nil && (!c.confirmation || options.Condition == cache.PutUnconditional) {
		return c.writeErr
	}
	return c.Cache.Put(ctx, key, value, options)
}

func TestOrderFailures(t *testing.T) {
	for _, tt := range []struct {
		name        string
		store       cache.Cache
		apiStatus   int
		body        string
		code, calls int
		saved       ordertypes.TransferStatus
	}{
		{name: "API failure", store: cache.NewInMemoryCache(), apiStatus: 503, code: 303, calls: 1, saved: ordertypes.TransferFailed},
		{name: "invalid API result", store: cache.NewInMemoryCache(), apiStatus: 200, body: `{}`, code: 303, calls: 1, saved: ordertypes.TransferFailed},
		{name: "cache read", store: failingCache{Cache: cache.NewInMemoryCache(), readErr: errors.New("read failed")}, code: 500},
		{name: "cache decode", store: failingCache{Cache: cache.NewInMemoryCache(), body: `{`}, code: 500},
		{name: "reservation", store: failingCache{Cache: cache.NewInMemoryCache(), writeErr: errors.New("write failed")}, code: 409},
		{name: "confirmation", store: failingCache{Cache: cache.NewInMemoryCache(), writeErr: errors.New("write failed"), confirmation: true}, apiStatus: 200, body: `{"products_link_url":"https://www.instacart.com/list"}`, code: 500, calls: 1, saved: ordertypes.TransferPending},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			handler := testHandler(t, tt.store, transportFunc(func(_ *http.Request) (*http.Response, error) { calls++; return response(tt.apiStatus, tt.body), nil }))
			w := submit(handler, orderForm(t, orderInput(), "list-hash"))
			assert.Equal(t, tt.code, w.Code)
			assert.Equal(t, tt.calls, calls)
			if tt.saved != "" {
				record, err := loadOrderTransfer(t.Context(), tt.store, "mock-clerk-user-id", "list-hash")
				require.NoError(t, err)
				assert.Equal(t, tt.saved, record.Status)
				w = submit(handler, orderForm(t, orderInput(), "list-hash"))
				assert.Equal(t, 303, w.Code)
				assert.Equal(t, tt.calls, calls, "saved failures and pending records must not resubmit")
			}
		})
	}
}

func TestConcurrentSubmissionsReserveOnce(t *testing.T) {
	var calls atomic.Int32
	handler := testHandler(t, cache.NewInMemoryCache(), transportFunc(func(_ *http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(200, `{"products_link_url":"https://www.instacart.com/list"}`), nil
	}))
	values := orderForm(t, orderInput(), "list-hash")
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for range 10 {
		wg.Go(func() { codes <- submit(handler, values).Code })
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		assert.Contains(t, []int{303, 409}, code)
	}
	assert.EqualValues(t, 1, calls.Load())
}

func TestInvalidOrders(t *testing.T) {
	handler := testHandler(t, cache.NewInMemoryCache(), transportFunc(func(_ *http.Request) (*http.Response, error) { t.Fatal("invalid order reached API"); return nil, nil }))
	for _, key := range []string{"", " ", "..", ".", "bad/key", "bad\\key", strings.Repeat("x", 256)} {
		assert.Equal(t, 400, submit(handler, orderForm(t, orderInput(), key)).Code, key)
	}
	for _, back := range []string{"https://evil.test", "//evil.test", "/%2f%2fevil.test", "/\\evil.test", "/%0d%0aLocation:evil", "recipes"} {
		input := orderInput()
		input.ReturnURL = back
		assert.Equal(t, 400, submit(handler, orderForm(t, input, "list-hash")).Code, back)
	}
	for _, raw := range []string{`null`, `{`, `{"location":{"id":"store"},"return_url":"/recipes","items":[]}`, `{"location":{"id":"store"},"return_url":"/recipes","items":[{"name":" "}]}`, `{"return_url":"/recipes","items":[{"name":"Food"}]}`, strings.Repeat("x", (1<<20)+1)} {
		assert.Equal(t, 400, submit(handler, url.Values{"transfer_request": {raw}, "idempotency_key": {"list-hash"}}).Code)
	}
	// The old form contract is intentionally gone.
	assert.Equal(t, 400, submit(handler, url.Values{"items": {`[{"name":"Food"}]`}, "location": {`{"id":"store"}`}, "return_url": {"/recipes"}, "list_key": {"list"}}).Code)
}

func TestSignInAndCrossOrigin(t *testing.T) {
	handler := testHandler(t, cache.NewInMemoryCache(), transportFunc(func(_ *http.Request) (*http.Response, error) { t.Fatal("blocked request reached API"); return nil, nil }))
	handler.auth = requestAuth{AuthClient: auth.DefaultMock(), err: auth.ErrNoSession}
	w := submit(handler, orderForm(t, orderInput(), "list-hash"))
	require.Equal(t, 303, w.Code)
	u, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/sign-in", u.Path)
	decoded, err := base64.RawURLEncoding.DecodeString(u.Query().Get("return_to_b64"))
	require.NoError(t, err)
	assert.Equal(t, orderInput().ReturnURL, string(decoded))
	handler.auth = requestAuth{AuthClient: auth.DefaultMock(), err: errors.New("account lookup failed")}
	assert.Equal(t, 500, submit(handler, orderForm(t, orderInput(), "list-hash")).Code)
	handler.auth = auth.DefaultMock()
	routes := http.NewServeMux()
	handler.Register(routes)
	r := httptest.NewRequest("POST", "https://careme.test"+orderTransferURL, strings.NewReader(orderForm(t, orderInput(), "list-hash").Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://evil.test")
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, r)
	assert.Equal(t, 403, w.Code)
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("GET", orderTransferURL, nil))
	assert.Equal(t, 405, w.Code)
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("POST", "/instacart/order", nil))
	assert.Equal(t, 404, w.Code)
}

func TestOrderViewUsesSharedFormAndSavedStates(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	input := ordertypes.OrderViewInput{UserID: "viewer", ListKey: "list", Items: orderInput().Items, Location: orderInput().Location, ReturnURL: orderInput().ReturnURL}
	store := cache.NewInMemoryCache()
	view, err := OrderViewForUser(t.Context(), store, input)
	require.NoError(t, err)
	assert.Equal(t, orderTransferURL, view.TransferURL)
	assert.Equal(t, "list", view.IdempotencyKey)
	assert.Equal(t, "Shop with Instacart", view.TransferLabel)
	var posted ordertypes.TransferRequest
	require.NoError(t, json.Unmarshal([]byte(view.TransferRequestJSON), &posted))
	assert.Equal(t, orderInput(), posted)
	var page bytes.Buffer
	require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_order_form", view))
	assert.Contains(t, page.String(), `hx-boost="false"`)
	assert.Contains(t, page.String(), `name="transfer_request"`)
	assert.Contains(t, page.String(), `name="idempotency_key"`)
	assert.NotContains(t, page.String(), `name="items"`)
	assert.NotContains(t, page.String(), "hx-post")
	for _, tt := range []struct {
		status ordertypes.TransferStatus
		failed bool
		link   string
	}{
		{ordertypes.TransferPending, false, ""}, {ordertypes.TransferApplied, false, "https://www.instacart.com/list"}, {ordertypes.TransferFailed, true, ""},
	} {
		require.NoError(t, saveOrderTransfer(t.Context(), store, "viewer", "list", ordertypes.Transfer{TransferID: "list", Status: tt.status, CartURL: tt.link}, cache.Unconditional()))
		view, err = OrderViewForUser(t.Context(), store, input)
		require.NoError(t, err)
		assert.Equal(t, tt.failed, view.TransferFailed)
		assert.Equal(t, tt.link, view.ReviewCartURL)
		assert.Empty(t, view.TransferURL)
		page.Reset()
		require.NoError(t, templates.ShoppingList.ExecuteTemplate(&page, "shopping_order_form", view))
		if tt.failed {
			assert.Contains(t, page.String(), "Add to cart failed.")
		}
		if tt.link != "" {
			assert.Contains(t, page.String(), `href="https://www.instacart.com/list"`)
		}
		assert.NotContains(t, page.String(), "<form")
	}
	// A viewer never reads another user's result.
	input.UserID = "another-viewer"
	view, err = OrderViewForUser(t.Context(), store, input)
	require.NoError(t, err)
	assert.Equal(t, orderTransferURL, view.TransferURL)
}

func TestOrderViewErrors(t *testing.T) {
	input := ordertypes.OrderViewInput{UserID: "viewer", ListKey: "list"}
	for _, store := range []cache.Cache{
		failingCache{Cache: cache.NewInMemoryCache(), readErr: errors.New("read failed")},
		failingCache{Cache: cache.NewInMemoryCache(), body: `{`},
		failingCache{Cache: cache.NewInMemoryCache(), body: `{"status":"unknown"}`},
	} {
		_, err := OrderViewForUser(t.Context(), store, input)
		require.Error(t, err)
	}
	input.UserID = ""
	_, err := OrderViewForUser(t.Context(), cache.NewInMemoryCache(), input)
	require.ErrorContains(t, err, "authenticated user")
	invalid := math.NaN()
	input.UserID = "viewer"
	input.Items = []ai.ShoppingItem{{Name: "Food", Buy: &invalid}}
	_, err = OrderViewForUser(t.Context(), cache.NewInMemoryCache(), input)
	require.ErrorContains(t, err, "encode Instacart transfer request")
}
