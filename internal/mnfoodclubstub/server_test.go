package mnfoodclubstub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI(t *testing.T) {
	h := New("https://test.careme.cooking/mnfoodclub-stub")
	call := func(method, path, key, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/mnfoodclub-stub"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"items":[{"product_id":"<script>bad</script>","variant_id":"v"}],"identity_hints":{"emails":["chef@example.com"]}}`
	first := call("POST", "/v1/cart-transfers", "key", body, TestToken)
	require.Equal(t, 200, first.Code)
	var result Response
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &result))
	require.Len(t, result.Items, 1)
	assert.Equal(t, 1, result.Items[0].AcceptedQuantity)
	assert.Equal(t, first.Body.String(), call("POST", "/v1/cart-transfers", "key", body, TestToken).Body.String())
	assert.Equal(t, first.Body.String(), call("GET", "/v1/cart-transfers/"+result.TransferID, "", "", TestToken).Body.String())
	assert.Equal(t, 409, call("POST", "/v1/cart-transfers", "key", `{"items":[{"product_id":"other"}]}`, TestToken).Code)
	assert.Equal(t, 401, call("GET", "/v1/cart-transfers/"+result.TransferID, "", "", "bad").Code)
	assert.Equal(t, 404, call("GET", "/v1/cart-transfers/unknown", "", "", TestToken).Code)
	for _, bad := range []string{`{`, `{"items":[]}`, `{"items":[{"product_id":"x","quantity":0}]}`, `{"items":[{"product_id":"x","quantity":-1}]}`, `{"items":[{"product_id":"x"},{"product_id":"x"}]}`, `{"items":[{"product_id":""}]}`} {
		assert.Equal(t, 400, call("POST", "/v1/cart-transfers", "invalid", bad, TestToken).Code)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			assert.Equal(t, first.Body.String(), call("POST", "/v1/cart-transfers", "key", body, TestToken).Body.String())
		})
	}
	wg.Wait()
	page := call("GET", "/", "", "", "")
	redirect := call("GET", "", "", "", "")
	assert.Equal(t, http.StatusMovedPermanently, redirect.Code)
	assert.Equal(t, "/mnfoodclub-stub/", redirect.Header().Get("Location"))
	assert.Contains(t, page.Body.String(), "&lt;script&gt;")
	assert.NotContains(t, page.Body.String(), "<script>")
	assert.Contains(t, page.Body.String(), "chef@example.com")
	fresh := httptest.NewRecorder()
	New("https://test.careme.cooking/mnfoodclub-stub").ServeHTTP(fresh, httptest.NewRequest(http.MethodGet, "/mnfoodclub-stub/", nil))
	assert.Contains(t, fresh.Body.String(), "No orders yet")
}
