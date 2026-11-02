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
	databaseURL := testDatabaseURL(t)
	store, err := OpenStore(t.Context(), databaseURL)
	require.NoError(t, err)
	publicURL := "https://test.careme.cooking/mnfoodclub-stub"
	call := func(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/mnfoodclub-stub"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Idempotency-Key", "persistent-key")
		r.AddCookie(testNameCookie("Existing Chef"))
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
	reopened, err := OpenStore(t.Context(), databaseURL)
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
	second.PostResponse.TransferID = "second-id"
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
		other, err := OpenStore(t.Context(), databaseURL)
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
				r.AddCookie(testNameCookie("Existing Chef"))
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

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	databaseURL := os.Getenv("MNFOODCLUB_STUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MNFOODCLUB_STUB_TEST_DATABASE_URL to run CockroachDB persistence tests")
	}
	admin, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	schema := "stub_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(t.Context(), "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		assert.NoError(t, err)
	})
	u, err := url.Parse(databaseURL)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func TestSQLPendingLoginSurvivesRestart(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	open := func() *sqlStore {
		store, err := OpenStore(t.Context(), databaseURL)
		require.NoError(t, err)
		t.Cleanup(store.Close)
		return store
	}
	publicURL := "https://test.careme.cooking/mnfoodclub-stub"
	store := open()
	client := stubTestClient{handler: New(publicURL, store)}
	initial := client.call("POST", "/v1/cart-transfers", "pending-sql", loginRequest)
	pending := decodeResponse(t, initial)
	assert.Equal(t, pendingLogin, pending.Status)
	store.Close()
	store = open()
	client.handler = New(publicURL, store)
	assert.Equal(t, pending, decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+pending.TransferID, "", "")))
	loggedIn := client.call("POST", "/handoff/"+pending.TransferID, "", "name=SQL+Chef")
	require.Equal(t, 303, loggedIn.Code, loggedIn.Body.String())
	applied := decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+pending.TransferID, "", ""))
	assert.Equal(t, "SQL Chef", applied.UserID)
	require.Len(t, applied.Items, 2)
	assert.Equal(t, 2, applied.Items[0].AcceptedQuantity)
	store.Close()
	store = open()
	client.handler = New(publicURL, store)
	assert.Equal(t, applied, decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+pending.TransferID, "", "")))
	assert.Equal(t, initial.Body.String(), client.call("POST", "/v1/cart-transfers", "pending-sql", loginRequest).Body.String())
	assert.Equal(t, 303, client.call("POST", "/handoff/"+pending.TransferID, "", "name=Another+Chef").Code)
	assert.Equal(t, applied, decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+pending.TransferID, "", "")))
	// Simultaneous login through separate pools must pick exactly one owner.
	next := decodeResponse(t, client.call("POST", "/v1/cart-transfers", "login-race", loginRequest))
	other := open()
	clients := []stubTestClient{client, {handler: New(publicURL, other)}}
	var concurrent sync.WaitGroup
	for i := range 20 {
		concurrent.Go(func() {
			w := clients[i%2].call("POST", "/handoff/"+next.TransferID, "", url.Values{"name": {fmt.Sprintf("Chef %d", i)}}.Encode())
			assert.Equal(t, 303, w.Code, w.Body.String())
		})
	}
	concurrent.Wait()
	winner := decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+next.TransferID, "", ""))
	assert.NotEmpty(t, winner.UserID)
	assert.Equal(t, "applied", string(winner.Status))
	require.Len(t, winner.Items, 2)
	assert.Equal(t, 303, client.call("POST", "/handoff/"+next.TransferID, "", "name=Last+Chef").Code)
	assert.Equal(t, winner, decodeResponse(t, client.call("GET", "/v1/cart-transfers/"+next.TransferID, "", "")))
}

func TestSQLMigrationPreservesLegacyAppliedTransfers(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	legacy, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(legacy.Close)
	_, err = legacy.Exec(t.Context(), `CREATE TABLE mnfoodclub_stub_transfers(id TEXT PRIMARY KEY,idempotency_key TEXT NOT NULL UNIQUE,created_at TIMESTAMPTZ NOT NULL,request JSONB NOT NULL,response JSONB NOT NULL)`)
	require.NoError(t, err)
	requestJSON := `{"items":[{"product_id":"101","quantity":1}],"identity_hints":{"emails":null}}`
	responseJSON := `{"transfer_id":"legacy","status":"applied","user_id":"stub-customer","cart_url":"https://test.careme.cooking/mnfoodclub-stub/#legacy","items":[{"product_id":"101","accepted_quantity":1,"rejected_quantity":0}]}`
	_, err = legacy.Exec(t.Context(), `INSERT INTO mnfoodclub_stub_transfers VALUES($1,$2,$3,$4,$5)`, "legacy", "legacy-key", time.Now(), []byte(requestJSON), []byte(responseJSON))
	require.NoError(t, err)
	store, err := OpenStore(t.Context(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	client := stubTestClient{handler: New("https://test.careme.cooking/mnfoodclub-stub", store)}
	got := decodeResponse(t, client.call("GET", "/v1/cart-transfers/legacy", "", ""))
	assert.Equal(t, "stub-customer", got.UserID)
	assert.Equal(t, got, decodeResponse(t, client.call("POST", "/v1/cart-transfers", "legacy-key", `{"items":[{"product_id":"101"}]}`)))
	assert.Equal(t, 303, client.call("POST", "/handoff/legacy", "", "name=New+Chef").Code)
	assert.Equal(t, got, decodeResponse(t, client.call("GET", "/v1/cart-transfers/legacy", "", "")))
}
