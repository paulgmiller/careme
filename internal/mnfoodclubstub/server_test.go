package mnfoodclubstub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI(t *testing.T) {
	store := newMemoryStore()
	h := New("https://test.careme.cooking/mnfoodclub-stub", store)
	call := func(method, path, key, body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/mnfoodclub-stub"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := `{"items":[{"product_id":"<script>bad</script>","variant_id":"v"}],"identity_hints":{"emails":["chef@example.com"]}}`
	first := call("POST", "/v1/cart-transfers", "key", body, testToken)
	require.Equal(t, 200, first.Code)
	var result response
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &result))
	require.Len(t, result.Items, 1)
	assert.Equal(t, 1, result.Items[0].AcceptedQuantity)
	assert.Equal(t, first.Body.String(), call("POST", "/v1/cart-transfers", "key", body, testToken).Body.String())
	assert.Equal(t, first.Body.String(), call("GET", "/v1/cart-transfers/"+result.TransferID, "", "", testToken).Body.String())
	assert.Equal(t, 409, call("POST", "/v1/cart-transfers", "key", `{"items":[{"product_id":"other"}]}`, testToken).Code)
	assert.Equal(t, 401, call("GET", "/v1/cart-transfers/"+result.TransferID, "", "", "bad").Code)
	assert.Equal(t, 404, call("GET", "/v1/cart-transfers/unknown", "", "", testToken).Code)
	for _, bad := range []string{`{`, `{"items":[]}`, `{"items":[{"product_id":"x","quantity":0}]}`, `{"items":[{"product_id":"x","quantity":-1}]}`, `{"items":[{"product_id":"x"},{"product_id":"x"}]}`, `{"items":[{"product_id":""}]}`} {
		assert.Equal(t, 400, call("POST", "/v1/cart-transfers", "invalid", bad, testToken).Code)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			assert.Equal(t, first.Body.String(), call("POST", "/v1/cart-transfers", "key", body, testToken).Body.String())
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
	New("https://test.careme.cooking/mnfoodclub-stub", store).ServeHTTP(fresh, httptest.NewRequest(http.MethodGet, "/mnfoodclub-stub/", nil))
	assert.Contains(t, fresh.Body.String(), result.TransferID)
}

// Only HTTP unit tests use an in-memory store. The deployed stub requires SQL.
type memoryStore struct {
	mu      sync.Mutex
	records []record
	err     error
}

func newMemoryStore() *memoryStore { return &memoryStore{} }
func (s *memoryStore) create(_ context.Context, candidate record) (record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return record{}, s.err
	}
	for _, rec := range s.records {
		if rec.Key == candidate.Key {
			return rec, nil
		}
	}
	s.records = append(s.records, candidate)
	return candidate, nil
}

func (s *memoryStore) get(_ context.Context, id string) (record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return record{}, s.err
	}
	for _, rec := range s.records {
		if rec.Response.TransferID == id {
			return rec, nil
		}
	}
	return record{}, errNotFound
}

func (s *memoryStore) list(context.Context) ([]record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	records := slices.Clone(s.records)
	slices.Reverse(records)
	return records, nil
}
func (s *memoryStore) ping(context.Context) error { return s.err }

func TestStorageFailures(t *testing.T) {
	store := newMemoryStore()
	store.err = errors.New("database unavailable")
	h := New("https://test.careme.cooking/mnfoodclub-stub", store)
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/ready", "", 503},
		{"GET", "/live", "", 200},
		{"GET", "/", "", 500},
		{"GET", "/v1/cart-transfers/id", "", 500},
		{"POST", "/v1/cart-transfers", `{"items":[{"product_id":"101"}]}`, 500},
	} {
		t.Run(tc.path+tc.method, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/mnfoodclub-stub"+tc.path, strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer "+testToken)
			r.Header.Set("Idempotency-Key", "key")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			assert.Equal(t, tc.status, w.Code)
			assert.NotContains(t, w.Body.String(), store.err.Error())
		})
	}
}
