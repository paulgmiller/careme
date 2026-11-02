package mnfoodclubstub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLStorePersistence(t *testing.T) {
	databaseURL := os.Getenv("MNFOODCLUB_STUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MNFOODCLUB_STUB_TEST_DATABASE_URL to run CockroachDB persistence tests")
	}
	admin, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	defer admin.Close()
	schema := "stub_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(t.Context(), "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	defer func() {
		_, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		assert.NoError(t, err)
	}()
	u, err := url.Parse(databaseURL)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	store, err := OpenStore(t.Context(), u.String())
	require.NoError(t, err)
	publicURL := "https://test.careme.cooking/mnfoodclub-stub"
	call := func(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/mnfoodclub-stub"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Idempotency-Key", "persistent-key")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"items":[{"product_id":"101","variant_id":"1005","quantity":3}],"identity_hints":{"emails":["chef@example.test"],"zip_code":"00501"}}`
	first := call(New(publicURL, store), "POST", "/v1/cart-transfers", body)
	require.Equal(t, 200, first.Code, first.Body.String())
	var result response
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &result))
	store.Close()
	// A new pool and new server must retain IDs, hints, quantities and idempotency.
	reopened, err := OpenStore(t.Context(), u.String())
	require.NoError(t, err)
	defer reopened.Close()
	h := New(publicURL, reopened)
	got := call(h, "GET", "/v1/cart-transfers/"+result.TransferID, "")
	assert.Equal(t, first.Body.String(), got.Body.String())
	assert.Equal(t, first.Body.String(), call(h, "POST", "/v1/cart-transfers", body).Body.String())
	assert.Equal(t, 409, call(h, "POST", "/v1/cart-transfers", `{"items":[{"product_id":"changed"}]}`).Code)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			w := call(h, "POST", "/v1/cart-transfers", body)
			assert.Equal(t, 200, w.Code, w.Body.String())
			assert.Equal(t, first.Body.String(), w.Body.String())
		})
	}
	wg.Wait()
	records, err := reopened.list(t.Context())
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "00501", records[0].Request.IdentityHints.ZipCode)
	assert.Equal(t, 3, records[0].Response.Items[0].AcceptedQuantity)
	// Different keys must remain distinct and the public page is newest first.
	second := records[0]
	second.Key = "second-key"
	second.Response.TransferID = "second-id"
	second.Created = second.Created.Add(time.Second)
	_, err = reopened.create(t.Context(), second)
	require.NoError(t, err)
	records, err = reopened.list(t.Context())
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "second-key", records[0].Key)
	page := call(h, "GET", "/", "")
	assert.Equal(t, 200, page.Code)
	assert.Contains(t, page.Body.String(), "00501")

	t.Run("concurrent first requests across pools", func(t *testing.T) {
		other, err := OpenStore(t.Context(), u.String())
		require.NoError(t, err)
		defer other.Close()
		handlers := []http.Handler{h, New(publicURL, other)}
		ids := make(chan string, 20)
		var concurrent sync.WaitGroup
		for i := range 20 {
			concurrent.Go(func() {
				r := httptest.NewRequest(http.MethodPost, "/mnfoodclub-stub/v1/cart-transfers", strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+testToken)
				r.Header.Set("Idempotency-Key", "concurrent-key")
				w := httptest.NewRecorder()
				handlers[i%len(handlers)].ServeHTTP(w, r)
				assert.Equal(t, 200, w.Code, w.Body.String())
				var result response
				if assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result)) {
					ids <- result.TransferID
				}
			})
		}
		concurrent.Wait()
		close(ids)
		unique := map[string]bool{}
		for id := range ids {
			unique[id] = true
		}
		assert.Len(t, unique, 1)
		records, err := reopened.list(t.Context())
		require.NoError(t, err)
		assert.Len(t, records, 3)
	})
	reopened.Close()
	assert.Equal(t, 503, call(h, "GET", "/ready", "").Code)
	assert.Equal(t, 500, call(h, "GET", "/v1/cart-transfers/"+result.TransferID, "").Code)
}

func TestOpenStoreRequiresValidDatabaseURL(t *testing.T) {
	for _, value := range []string{"", "://bad"} {
		t.Run(fmt.Sprint(len(value)), func(t *testing.T) { _, err := OpenStore(t.Context(), value); require.Error(t, err) })
	}
}
