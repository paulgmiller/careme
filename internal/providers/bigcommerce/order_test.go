package bigcommerce

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

func newTestHandler(t *testing.T, store cache.Cache, transport http.RoundTripper) *OrderHandler {
	t.Helper()
	handler, err := NewOrderHandler(OrderOptions{Cache: store, Client: testClient(t, transport), Auth: auth.DefaultMock(), LocationPrefix: "mnfoodclub_"})
	require.NoError(t, err)
	return handler
}

func form() url.Values {
	return url.Values{"items": {`[{"id":"123","name":"Chicken","needed":"2 lbs","buy":1.2},{"name":"Oil","needed":"1 tbsp"},{"id":"mnfoodclub_seasonal-leeks","name":"Leeks","needed":"2"}]`}, "location": {`{"id":"mnfoodclub_delivery","name":"MNFoodClub"}`}, "list_key": {"list-hash"}, "return_url": {"/recipes?h=list-hash#shopping-list-section"}}
}

func submit(a *OrderHandler, values url.Values) *httptest.ResponseRecorder {
	routes := http.NewServeMux()
	a.Register(routes)
	r := httptest.NewRequest(http.MethodPost, "https://careme.test/bigcommerce/order", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, r)
	return w
}

func status(t *testing.T, w *httptest.ResponseRecorder) url.Values {
	t.Helper()
	require.Equal(t, 303, w.Code)
	u, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/recipes", u.Path)
	assert.Equal(t, "shopping-list-section", u.Fragment)
	assert.Equal(t, "list-hash", u.Query().Get("h"))
	return u.Query()
}

func review(a *OrderHandler, id string) *httptest.ResponseRecorder {
	routes := http.NewServeMux()
	a.Register(routes)
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("GET", "https://careme.test/bigcommerce/cart?transfer="+url.QueryEscape(id), nil))
	return w
}

func TestOrderAndFreshReviewLinks(t *testing.T) {
	carts, lookups, redirects := 0, 0, 0
	store := cache.NewInMemoryCache()
	handler := newTestHandler(t, store, transportFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/stores/test/v3/catalog/products/123":
			lookups++
			return response(200, availableProduct), nil
		case "/stores/test/v3/carts":
			carts++
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.JSONEq(t, `{"customer_id":0,"line_items":[{"product_id":123,"variant_id":11,"quantity":2}]}`, string(body))
			return response(201, `{"data":{"id":"`+testCartID+`"}}`), nil
		case "/stores/test/v3/carts/" + testCartID + "/redirect_urls":
			redirects++
			return response(201, fmt.Sprintf(`{"data":{"cart_url":"%s/cart.php?action=load&token=%d"}}`, testOrigin, redirects)), nil
		default:
			t.Fatalf("unexpected request %s", r.URL)
			return nil, nil
		}
	}))
	first := status(t, submit(handler, form()))
	assert.Equal(t, "complete", first.Get("bigcommerce_status"))
	assert.Equal(t, "1", first.Get("bigcommerce_added"))
	assert.Equal(t, "2", first.Get("bigcommerce_unmatched"))
	second := status(t, submit(handler, form()))
	assert.Equal(t, first, second)
	assert.Equal(t, 1, carts)
	assert.Equal(t, 1, lookups)
	for i := 1; i <= 2; i++ {
		w := review(handler, first.Get("bigcommerce_transfer"))
		assert.Equal(t, 303, w.Code)
		assert.Equal(t, fmt.Sprintf("%s/cart.php?action=load&token=%d", testOrigin, i), w.Header().Get("Location"))
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
	assert.Equal(t, 2, redirects)
	assert.Equal(t, 1, carts)
	keys, err := store.List(t.Context(), "bigcommerce_cart_transfers/", "")
	require.NoError(t, err)
	require.Len(t, keys, 1)
	reader, err := store.Get(t.Context(), "bigcommerce_cart_transfers/"+keys[0])
	require.NoError(t, err)
	cached, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.NotContains(t, string(cached), "token=")
	assert.Contains(t, string(cached), testCartID)
	changed := form()
	changed.Set("items", `[{"id":"123","name":"Chicken","buy":3}]`)
	result := status(t, submit(handler, changed))
	assert.Equal(t, "changed", result.Get("bigcommerce_status"))
	assert.NotEmpty(t, result.Get("bigcommerce_transfer"))
	assert.Equal(t, 1, carts)
	handler.auth = requestAuth{AuthClient: auth.DefaultMock(), user: "another-user"}
	assert.Equal(t, 404, review(handler, first.Get("bigcommerce_transfer")).Code)
}

func TestUnknownAmountsUseOnePackage(t *testing.T) {
	handler := newTestHandler(t, cache.NewInMemoryCache(), transportFunc(func(_ *http.Request) (*http.Response, error) { return response(200, availableProduct), nil }))
	lines, gaps, err := handler.resolve(t.Context(), []ai.ShoppingItem{{ProductID: "123", Name: "Chicken", Needed: "2 lbs"}})
	require.NoError(t, err)
	assert.Zero(t, gaps)
	require.Len(t, lines, 1)
	assert.Equal(t, 1, lines[0].Quantity)
}

func TestConcurrentSubmissionsCreateOnlyOneCart(t *testing.T) {
	var carts atomic.Int32
	handler := newTestHandler(t, cache.NewInMemoryCache(), transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return response(200, availableProduct), nil
		}
		carts.Add(1)
		return response(201, `{"data":{"id":"`+testCartID+`"}}`), nil
	}))
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for range 10 {
		wg.Go(func() { codes <- submit(handler, form()).Code })
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		assert.Equal(t, 303, code)
	}
	assert.EqualValues(t, 1, carts.Load())
}

type failingCache struct {
	cache.Cache
	readErr, writeErr error
	failConfirmation  bool
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
	if c.writeErr != nil && (!c.failConfirmation || options.Condition == cache.PutUnconditional) {
		return c.writeErr
	}
	return c.Cache.Put(ctx, key, value, options)
}

func TestOrderFailurePaths(t *testing.T) {
	for _, tt := range []struct {
		name                     string
		store                    cache.Cache
		lookupStatus, cartStatus int
		cartErr                  error
		want                     string
		carts                    int
	}{
		{name: "lookup failure", store: cache.NewInMemoryCache(), lookupStatus: 503, want: "lookup_failed"},
		{name: "unavailable product", store: cache.NewInMemoryCache(), lookupStatus: 404, want: "no_matches"},
		{name: "API cart failure", store: cache.NewInMemoryCache(), lookupStatus: 200, cartStatus: 422, want: "uncertain", carts: 1},
		{name: "lost cart response", store: cache.NewInMemoryCache(), lookupStatus: 200, cartErr: errors.New("response lost"), want: "uncertain", carts: 1},
		{name: "corrupt cart response", store: cache.NewInMemoryCache(), lookupStatus: 200, cartStatus: 201, want: "uncertain", carts: 1},
		{name: "cache read failure", store: failingCache{readErr: errors.New("cache down")}, want: "start_failed"},
		{name: "cache decode failure", store: failingCache{body: `{`}, want: "start_failed"},
		{name: "reservation failure", store: failingCache{Cache: cache.NewInMemoryCache(), writeErr: errors.New("cache down")}, lookupStatus: 200, want: "start_failed"},
		{name: "confirmation failure", store: failingCache{Cache: cache.NewInMemoryCache(), writeErr: errors.New("cache down"), failConfirmation: true}, lookupStatus: 200, cartStatus: 201, want: "confirmation_failed", carts: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			carts := 0
			handler := newTestHandler(t, tt.store, transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "GET" {
					return response(tt.lookupStatus, availableProduct), nil
				}
				carts++
				if tt.cartErr != nil {
					return nil, tt.cartErr
				}
				body := `{}`
				if tt.name == "confirmation failure" {
					body = `{"data":{"id":"` + testCartID + `"}}`
				}
				return response(tt.cartStatus, body), nil
			}))
			q := status(t, submit(handler, form()))
			assert.Equal(t, tt.want, q.Get("bigcommerce_status"))
			assert.Equal(t, tt.carts, carts)
			if tt.carts > 0 {
				q = status(t, submit(handler, form()))
				assert.Contains(t, []string{"pending", "uncertain"}, q.Get("bigcommerce_status"))
				assert.Equal(t, 1, carts)
			}
		})
	}
}

func TestNoCatalogMatches(t *testing.T) {
	handler := newTestHandler(t, cache.NewInMemoryCache(), transportFunc(func(_ *http.Request) (*http.Response, error) { t.Fatal("unmatched items reached API"); return nil, nil }))
	values := form()
	values.Set("items", `[{"name":"Salt"},{"id":"mnfoodclub_produce-leeks","name":"Leeks"}]`)
	q := status(t, submit(handler, values))
	assert.Equal(t, "no_matches", q.Get("bigcommerce_status"))
	assert.Equal(t, "2", q.Get("bigcommerce_unmatched"))
}

func TestInvalidOrders(t *testing.T) {
	handler := newTestHandler(t, cache.NewInMemoryCache(), transportFunc(func(_ *http.Request) (*http.Response, error) { t.Fatal("invalid order reached API"); return nil, nil }))
	for _, tt := range []struct{ field, value string }{
		{"return_url", "https://evil.test"},
		{"return_url", "//evil.test"},
		{"return_url", "/%2f%2fevil.test"},
		{"return_url", "/\\evil.test"},
		{"return_url", "/%0d%0aevil"},
		{"location", `{"id":"walmart_123"}`},
		{"location", `{`},
		{"location", `null`},
		{"items", `[]`},
		{"items", `null`},
		{"items", `{`},
		{"items", `[{"name":" "}]`},
		{"items", `[{"name":"Food","buy":0}]`},
		{"items", `[{"name":"Food","buy":-1}]`},
		{"items", `[{"name":"Food","buy":2147483648}]`},
		{"list_key", ""},
	} {
		t.Run(tt.field+tt.value, func(t *testing.T) {
			values := form()
			values.Set(tt.field, tt.value)
			assert.Equal(t, 400, submit(handler, values).Code)
		})
	}
	duplicate := form()
	duplicate.Set("items", `[{"id":"123","name":"Food"},{"id":"123","name":"Food again"}]`)
	// A duplicate in the merged list fails before any cart write.
	handler.client = testClient(t, transportFunc(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "GET", r.Method)
		return response(200, availableProduct), nil
	}))
	assert.Equal(t, "lookup_failed", status(t, submit(handler, duplicate)).Get("bigcommerce_status"))
	oversized := form()
	oversized.Set("items", strings.Repeat("x", (1<<20)+1))
	assert.Equal(t, 400, submit(handler, oversized).Code)
}

type requestAuth struct {
	auth.AuthClient
	user string
	err  error
}

func (a requestAuth) GetUserIDFromRequest(_ *http.Request) (string, error) { return a.user, a.err }

func TestAuthenticationAndOrigin(t *testing.T) {
	handler := newTestHandler(t, cache.NewInMemoryCache(), transportFunc(func(_ *http.Request) (*http.Response, error) { t.Fatal("blocked request reached API"); return nil, nil }))
	for _, tt := range []struct {
		err  error
		code int
	}{{auth.ErrNoSession, 401}, {errors.New("auth failed"), 500}} {
		handler.auth = requestAuth{AuthClient: auth.DefaultMock(), err: tt.err}
		assert.Equal(t, tt.code, submit(handler, form()).Code)
		assert.Equal(t, tt.code, review(handler, strings.Repeat("a", 64)).Code)
	}
	handler.auth = auth.DefaultMock()
	routes := http.NewServeMux()
	handler.Register(routes)
	r := httptest.NewRequest("POST", "https://careme.test/bigcommerce/order", strings.NewReader(form().Encode()))
	r.Header.Set("Origin", "https://evil.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, r)
	assert.Equal(t, 403, w.Code)
	assert.Equal(t, 400, review(handler, "../invalid").Code)
	assert.Equal(t, 404, review(handler, strings.Repeat("a", 64)).Code)
}

func TestCartReviewFailures(t *testing.T) {
	for _, tt := range []struct {
		name   string
		store  cache.Cache
		record transfer
		want   int
	}{
		{name: "cache failure", store: failingCache{readErr: errors.New("cache down")}, want: 500},
		{name: "pending", store: cache.NewInMemoryCache(), record: transfer{Status: "pending"}, want: 409},
		{name: "expired cart", store: cache.NewInMemoryCache(), record: transfer{Status: "complete", CartID: testCartID}, want: 502},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, tt.store, transportFunc(func(_ *http.Request) (*http.Response, error) { return response(404, `{}`), nil }))
			id := strings.Repeat("a", 64)
			if tt.record.Status != "" {
				require.NoError(t, handler.save(t.Context(), handler.key("mock-clerk-user-id", id), tt.record, cache.Unconditional()))
			}
			assert.Equal(t, tt.want, review(handler, id).Code)
		})
	}
}

func TestOrderViewAndTemplate(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}))
	for _, tt := range []struct{ code, want string }{
		{"complete", "Sent 2 matched products"}, {"changed", "list changed"}, {"pending", "in progress"}, {"uncertain", "could not confirm"}, {"no_matches", "Nothing was sent"}, {"lookup_failed", "Nothing was sent"}, {"confirmation_failed", "may have been created"}, {"<script>alert(1)</script>", ""},
	} {
		t.Run(tt.code, func(t *testing.T) {
			input := ordertypes.OrderViewInput{Location: locationtypes.Location{ID: "mnfoodclub_delivery"}, Items: []ai.ShoppingItem{{ProductID: "123", Name: `Chicken & "herbs"`}}, ListKey: "hash", ReturnURL: "/recipes?h=hash", Query: url.Values{"bigcommerce_status": {tt.code}, "bigcommerce_added": {"2"}, "bigcommerce_unmatched": {"1"}, "bigcommerce_transfer": {strings.Repeat("a", 64)}}}
			view, err := OrderView(input, "MNFoodClub")
			require.NoError(t, err)
			assert.Equal(t, "/bigcommerce/order", view.ActionURL)
			assert.Equal(t, "Add to MNFoodClub cart", view.ActionLabel)
			if tt.code == "complete" {
				assert.Contains(t, view.Notice, "1 item was not sent")
			}
			if tt.want != "" {
				assert.Contains(t, view.Notice, tt.want)
			} else {
				assert.Empty(t, view.Notice)
			}
			if tt.code == "complete" || tt.code == "changed" {
				assert.Equal(t, "/bigcommerce/cart?transfer="+strings.Repeat("a", 64), view.ReviewURL)
			} else {
				assert.Empty(t, view.ReviewURL)
			}
			var body bytes.Buffer
			data := map[string]any{"Orders": []*ordertypes.OrderView{view}, "Purchases": nil, "HasSavedRecipes": true}
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_finalize_controls", data))
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shopping_purchase_section", data))
			assert.Contains(t, body.String(), `action="/bigcommerce/order"`)
			assert.Contains(t, body.String(), "Add to MNFoodClub cart")
			assert.NotContains(t, body.String(), "<script>")
			assert.Contains(t, body.String(), `\u0026`)
			assert.Contains(t, body.String(), `&#34;herbs\&#34;`)
		})
	}
}

func TestTransferIdentityIncludesStoreAndUser(t *testing.T) {
	// Distinct store locations and list keys must not share cart records.
	var carts int
	handler := newTestHandler(t, cache.NewInMemoryCache(), transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return response(200, availableProduct), nil
		}
		carts++
		return response(201, `{"data":{"id":"`+testCartID+`"}}`), nil
	}))
	for _, location := range []string{"mnfoodclub_delivery", "mnfoodclub_pickup"} {
		values := form()
		values.Set("location", `{"id":"`+location+`"}`)
		status(t, submit(handler, values))
	}
	assert.Equal(t, 2, carts)
}

func TestRequiredHandlerDependencies(t *testing.T) {
	_, err := NewOrderHandler(OrderOptions{})
	require.Error(t, err)
}
