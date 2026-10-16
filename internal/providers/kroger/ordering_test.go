package kroger

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/ordering"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestOrderingAdapter(t *testing.T, client *CartClient) *OrderingAdapter {
	t.Helper()
	adapter, err := NewOrderingAdapter(OrderingOptions{Cache: cache.NewInMemoryCache(), Client: client, TokenKey: []byte(strings.Repeat("k", 32)), Origin: "https://careme.test", Auth: auth.DefaultMock(), Resume: func(http.ResponseWriter, *http.Request, string, string) {}})
	require.NoError(t, err)
	return adapter
}

func TestOrderingTokenEncryptionAndUserBinding(t *testing.T) {
	adapter := newTestOrderingAdapter(t, &CartClient{})
	token := CartToken{AccessToken: "secret-shopper-token", RefreshToken: "secret-refresh", ExpiresAt: time.Now().Add(time.Hour).UTC()}
	require.NoError(t, adapter.SaveToken(t.Context(), "user-one", token))
	reader, err := adapter.cache.Get(t.Context(), "kroger_cart_tokens/user-one")
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.NotContains(t, string(body), token.AccessToken)
	assert.NotContains(t, string(body), token.RefreshToken)
	loaded, err := adapter.LoadToken(t.Context(), "user-one")
	require.NoError(t, err)
	assert.Equal(t, token, loaded)
	require.NoError(t, adapter.cache.Put(t.Context(), "kroger_cart_tokens/user-two", string(body), cache.Unconditional()))
	_, err = adapter.LoadToken(t.Context(), "user-two")
	require.Error(t, err)
}

func TestOrderingPrepareAndSubmit(t *testing.T) {
	var added []CartItem
	calls := 0
	client := &http.Client{Transport: cartRoundTrip(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/cart/add":
			calls++
			assert.Equal(t, "Bearer shopper", req.Header.Get("Authorization"))
			var body struct {
				Items []CartItem `json:"items"`
			}
			require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
			added = body.Items
			return cartResponse(http.StatusNoContent, ""), nil
		case "/v1/products/missing":
			return cartResponse(http.StatusNotFound, `{}`), nil
		default:
			assert.Equal(t, "1234", req.URL.Query().Get("filter.locationId"))
			return cartResponse(http.StatusOK, `{"data":{"items":[{"itemId":"upc","inventory":{"stockLevel":"HIGH"}}]}}`), nil
		}
	})}
	manager := NewKrogerTokenManager("id", "secret", client)
	manager.token, manager.expiresAt = "catalog", time.Now().Add(time.Hour)
	adapter := newTestOrderingAdapter(t, &CartClient{HTTPClient: client, CatalogToken: manager})
	require.NoError(t, adapter.SaveToken(t.Context(), "user", CartToken{AccessToken: "shopper", ExpiresAt: time.Now().Add(time.Hour)}))
	request := ordering.Request{UserID: "user", Source: ordering.Store{Provider: "kroger", ID: "1234", Chain: "QFC"}, Items: []ordering.Item{
		{Provider: "kroger", ProductID: "first", Name: "Milk", Needed: "2 cups"},
		{Provider: "kroger", ProductID: "duplicate", Name: "More milk"},
		{Provider: "kroger", ProductID: "missing", Name: "Eggs"},
		{Name: "Bread"},
		{Provider: "instacart", ProductID: "foreign", Name: "Apples"},
	}}
	prepared, err := adapter.Prepare(t.Context(), request)
	require.NoError(t, err)
	assert.Zero(t, calls, "preparation must not write to the cart")
	require.Len(t, prepared.Sent, 1)
	assert.Equal(t, float64(1), prepared.Sent[0].PackageQuantity)
	require.Len(t, prepared.Gaps, 3)
	result, err := adapter.Submit(t.Context(), request, prepared)
	require.NoError(t, err)
	assert.Equal(t, []CartItem{{UPC: "upc", Quantity: 1}}, added)
	assert.Equal(t, 1, result.Added)
	assert.Equal(t, "https://www.qfc.com/cart", result.URL)
	assert.Equal(t, ordering.CartUpdated, result.Kind)
}

func TestOrderingPrepareErrors(t *testing.T) {
	t.Run("connection required", func(t *testing.T) {
		adapter := newTestOrderingAdapter(t, &CartClient{})
		_, err := adapter.Prepare(t.Context(), ordering.Request{UserID: "user"})
		var connection *ordering.ConnectionRequired
		require.ErrorAs(t, err, &connection)
		assert.Equal(t, "kroger", connection.Provider)
	})
	t.Run("lookup failure", func(t *testing.T) {
		client := &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
			return cartResponse(http.StatusServiceUnavailable, `{}`), nil
		})}
		manager := NewKrogerTokenManager("id", "secret", client)
		manager.token, manager.expiresAt = "catalog", time.Now().Add(time.Hour)
		adapter := newTestOrderingAdapter(t, &CartClient{HTTPClient: client, CatalogToken: manager})
		require.NoError(t, adapter.SaveToken(t.Context(), "user", CartToken{AccessToken: "shopper", ExpiresAt: time.Now().Add(time.Hour)}))
		_, err := adapter.Prepare(t.Context(), ordering.Request{UserID: "user", Items: []ordering.Item{{Provider: "kroger", ProductID: "product"}}})
		var step *ordering.StepError
		require.ErrorAs(t, err, &step)
		assert.Equal(t, "lookup_failed", step.Code)
	})
}

func TestOrderingAuthorizationStateAndCallback(t *testing.T) {
	adapter := newTestOrderingAdapter(t, &CartClient{ClientID: "client", RedirectURI: "https://careme.test/kroger/callback"})
	request := ordering.Request{UserID: "mock-clerk-user-id", Hash: "hash", Source: ordering.Store{Chain: "QFC"}}
	start := httptest.NewRecorder()
	adapter.StartAuthorization(start, httptest.NewRequest(http.MethodPost, "/recipes/hash/ordering/kroger", nil), request)
	require.Equal(t, http.StatusSeeOther, start.Code)
	assert.Contains(t, start.Header().Get("Location"), "banner=qfc")
	cookies := start.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.True(t, cookies[0].Secure)
	assert.True(t, cookies[0].HttpOnly)
	stateJSON, err := base64.RawURLEncoding.DecodeString(cookies[0].Value)
	require.NoError(t, err)
	var state krogerAuthState
	require.NoError(t, json.Unmarshal(stateJSON, &state))
	for _, nonce := range []string{"wrong", state.Nonce} {
		callback := httptest.NewRequest(http.MethodGet, "/kroger/callback?state="+nonce+"&error=access_denied", nil)
		callback.AddCookie(cookies[0])
		recorder := httptest.NewRecorder()
		adapter.Callback(recorder, callback)
		assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		if nonce == "wrong" {
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		} else {
			assert.Equal(t, http.StatusSeeOther, recorder.Code)
			assert.Contains(t, recorder.Header().Get("Location"), "kroger_error=denied")
		}
	}
}
