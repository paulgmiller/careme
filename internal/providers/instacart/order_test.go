package instacart

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
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testHandler(t *testing.T, store cache.Cache, transport http.RoundTripper) *OrderHandler {
	t.Helper()
	client, err := NewClient(ClientOptions{APIKey: "key", Transport: transport})
	require.NoError(t, err)
	handler, err := NewOrderHandler(OrderOptions{Cache: store, Client: client, Origin: "https://careme.test", Auth: auth.DefaultMock()})
	require.NoError(t, err)
	return handler
}

func orderForm() url.Values {
	return url.Values{
		"items":    {`[{"id":"not-an-instacart-id","name":"Chicken & herbs","needed":"2 lbs","buy":7},{"name":"Olive oil","needed":"4½ tbsp"},{"name":"Wine","needed":"1 bottle"}]`},
		"location": {`{"id":"any-store","name":"Any store","chain":"Local"}`},
		"list_key": {"list-hash"}, "return_url": {"/recipes?h=list-hash#shopping-list-section"},
	}
}

func submit(handler *OrderHandler, form url.Values) *httptest.ResponseRecorder {
	routes := http.NewServeMux()
	handler.Register(routes)
	r := httptest.NewRequest(http.MethodPost, "https://careme.test/instacart/order", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, r)
	return w
}

func TestOrderPayloadAndReuse(t *testing.T) {
	store := cache.NewInMemoryCache()
	calls := 0
	handler := testHandler(t, store, transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var payload shoppingList
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, "Careme shopping list", payload.Title)
		assert.Equal(t, "shopping_list", payload.LinkType)
		assert.Equal(t, 30, payload.ExpiresIn)
		assert.Equal(t, "https://careme.test/recipes?h=list-hash#shopping-list-section", payload.LandingPage.LinkbackURL)
		require.Len(t, payload.Items, 3)
		assert.Equal(t, lineItem{Name: "Chicken & herbs", DisplayText: "Chicken & herbs — 2 lbs", Measurements: []measurement{{Quantity: 2, Unit: "pound"}}}, payload.Items[0])
		assert.Equal(t, []measurement{{Quantity: 4.5, Unit: "tablespoon"}}, payload.Items[1].Measurements)
		assert.Equal(t, lineItem{Name: "Wine", DisplayText: "Wine — 1 bottle"}, payload.Items[2])
		return response(200, `{"products_link_url":"https://www.instacart.com/store/shopping_lists/1"}`), nil
	}))
	for range 2 {
		w := submit(handler, orderForm())
		assert.Equal(t, 303, w.Code)
		assert.Equal(t, "https://www.instacart.com/store/shopping_lists/1", w.Header().Get("Location"))
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
	assert.Equal(t, 1, calls)
	// The generated link is private to the authenticated user.
	handler.auth = requestAuth{AuthClient: auth.DefaultMock(), userID: "second-user"}
	assert.Equal(t, 303, submit(handler, orderForm()).Code)
	assert.Equal(t, 2, calls)
}

func TestLinkChangesAndExpiry(t *testing.T) {
	store := cache.NewInMemoryCache()
	calls := 0
	handler := testHandler(t, store, transportFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		return response(200, `{"products_link_url":"https://www.instacart.com/store/shopping_lists/1"}`), nil
	}))
	payload := shoppingList{Title: "list", Items: []lineItem{{Name: "Milk"}}}
	r := httptest.NewRequest("POST", "/instacart/order", nil)
	for range 2 {
		_, err := handler.shoppingLink(r, "user", payload)
		require.NoError(t, err)
	}
	assert.Equal(t, 1, calls)
	payload.Items[0].DisplayText = "Milk — 2 cups"
	_, err := handler.shoppingLink(r, "user", payload)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	keys, err := store.List(t.Context(), "instacart_shopping_links/user/", "")
	require.NoError(t, err)
	for _, key := range keys {
		record, err := json.Marshal(linkRecord{URL: "https://www.instacart.com/expired", ExpiresAt: time.Now().Add(-time.Hour)})
		require.NoError(t, err)
		require.NoError(t, store.Put(t.Context(), "instacart_shopping_links/user/"+key, string(record), cache.Unconditional()))
	}
	_, err = handler.shoppingLink(r, "user", payload)
	require.NoError(t, err)
	assert.Equal(t, 3, calls)
	handler.client.baseURL = DevelopmentURL
	_, err = handler.shoppingLink(r, "user", payload)
	require.NoError(t, err)
	assert.Equal(t, 4, calls)
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

func (c failingCache) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	return c.Cache.Put(ctx, key, value, opts)
}

func TestOrderFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cache  cache.Cache
		status int
		body   string
		calls  int
	}{
		{name: "API failure", cache: cache.NewInMemoryCache(), status: 503, calls: 1},
		{name: "invalid API result", cache: cache.NewInMemoryCache(), status: 200, body: `{}`, calls: 1},
		{name: "cache read", cache: failingCache{readErr: errors.New("read failed")}},
		{name: "cache decode", cache: failingCache{body: `{`}},
		{name: "cache unsafe link", cache: failingCache{body: `{"url":"https://evil.test"}`}},
		{name: "cache write", cache: failingCache{Cache: cache.NewInMemoryCache(), writeErr: errors.New("write failed")}, status: 200, body: `{"products_link_url":"https://www.instacart.com/list"}`, calls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			handler := testHandler(t, tt.cache, transportFunc(func(_ *http.Request) (*http.Response, error) { calls++; return response(tt.status, tt.body), nil }))
			w := submit(handler, orderForm())
			require.Equal(t, 303, w.Code)
			u, err := url.Parse(w.Header().Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, "/recipes", u.Path)
			assert.Equal(t, "list-hash", u.Query().Get("h"))
			assert.Equal(t, "failed", u.Query().Get("instacart_status"))
			assert.Equal(t, "shopping-list-section", u.Fragment)
			assert.Equal(t, tt.calls, calls)
		})
	}
}

func TestInvalidOrders(t *testing.T) {
	for _, tt := range []struct{ field, value string }{
		{"items", `[]`},
		{"items", `{`},
		{"items", `[{"name":" "}]`},
		{"items", "null"},
		{"location", `{`},
		{"location", `{}`},
		{"list_key", ""},
		{"return_url", "https://evil.test"},
		{"return_url", "//evil.test"},
		{"return_url", "/%2f%2fevil.test"},
		{"return_url", "/\\evil.test"},
		{"return_url", "/%0d%0aLocation:evil"},
	} {
		t.Run(tt.field+tt.value, func(t *testing.T) {
			handler := testHandler(t, cache.NewInMemoryCache(), transportFunc(func(_ *http.Request) (*http.Response, error) { t.Fatal("invalid order reached API"); return nil, nil }))
			form := orderForm()
			form.Set(tt.field, tt.value)
			assert.Equal(t, 400, submit(handler, form).Code)
		})
	}
}

func TestOrderAuthorizationAndCrossOrigin(t *testing.T) {
	handler := testHandler(t, cache.NewInMemoryCache(), transportFunc(func(_ *http.Request) (*http.Response, error) {
		t.Fatal("unauthorized order reached API")
		return nil, nil
	}))
	for _, tt := range []struct {
		err    error
		status int
	}{{auth.ErrNoSession, 401}, {errors.New("account lookup failed"), 500}} {
		handler.auth = requestAuth{AuthClient: auth.DefaultMock(), err: tt.err}
		assert.Equal(t, tt.status, submit(handler, orderForm()).Code)
	}
	handler.auth = auth.DefaultMock()
	routes := http.NewServeMux()
	handler.Register(routes)
	r := httptest.NewRequest("POST", "https://careme.test/instacart/order", strings.NewReader(orderForm().Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, r)
	assert.Equal(t, 403, w.Code)
	r = httptest.NewRequest("GET", "https://careme.test/instacart/order", nil)
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, r)
	assert.Equal(t, 405, w.Code)
	form := orderForm()
	form.Set("items", strings.Repeat("x", (1<<20)+1))
	assert.Equal(t, 400, submit(handler, form).Code)
}

func TestOrderViewNotice(t *testing.T) {
	for _, code := range []string{"failed", "complete", "<script>alert(1)</script>", ""} {
		view, err := OrderView(ordertypes.OrderViewInput{Query: url.Values{"instacart_status": {code}}})
		require.NoError(t, err)
		assert.Equal(t, "/instacart/order", view.ActionURL)
		if code == "failed" {
			assert.Equal(t, failureNotice, view.Notice)
		} else {
			assert.Empty(t, view.Notice)
		}
	}
}
