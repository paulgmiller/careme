package ordering

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"careme/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	id       string
	workflow Workflow
	prepare  func(context.Context, Request) (Prepared, error)
	submit   func(context.Context, Request, Prepared) (Result, error)
	calls    atomic.Int32
}

func (p *fakeProvider) ID() string                      { return p.id }
func (p *fakeProvider) Workflow() Workflow              { return p.workflow }
func (*fakeProvider) Supports(Request) bool             { return true }
func (*fakeProvider) Presentation(Request) Presentation { return Presentation{Brand: "Example"} }
func (p *fakeProvider) Prepare(ctx context.Context, r Request) (Prepared, error) {
	if p.prepare != nil {
		return p.prepare(ctx, r)
	}
	return Prepared{Sent: r.Items}, nil
}

func (p *fakeProvider) Submit(ctx context.Context, r Request, prepared Prepared) (Result, error) {
	p.calls.Add(1)
	if p.submit != nil {
		return p.submit(ctx, r, prepared)
	}
	if p.workflow == ShoppingListLink {
		return Result{Kind: ShoppingListCreated, URL: "https://example.com/review"}, nil
	}
	return Result{Kind: CartUpdated, URL: "https://example.com/review", Added: len(prepared.Sent)}, nil
}

func request() Request {
	return Request{UserID: "user", Hash: "list", Fingerprint: "snapshot", Title: "Dinner", LinkBackURL: "https://careme.test/recipes?h=list", Source: Store{Provider: "another", ID: "store"}, Items: []Item{{Name: "Milk", Needed: "2 cups", Measurements: []Measurement{{Quantity: 2, Unit: "cup"}}}}}
}

func TestCartTransferPartialMatchesAndChangedList(t *testing.T) {
	p := &fakeProvider{id: "cart", workflow: CartTransfer}
	p.prepare = func(_ context.Context, r Request) (Prepared, error) {
		sent := r.Items[0]
		sent.PackageQuantity = 1
		return Prepared{Sent: []Item{sent}, Gaps: []Item{{Name: "Salt"}}}, nil
	}
	s := New(cache.NewInMemoryCache(), []Provider{p})
	r := request()
	result, err := s.Transfer(t.Context(), "cart", r)
	require.NoError(t, err)
	assert.Equal(t, CartUpdated, result.Kind)
	assert.Equal(t, "complete", result.Status)
	assert.Equal(t, 1, result.Added)
	assert.Equal(t, float64(1), result.Sent[0].PackageQuantity)
	assert.Equal(t, "Salt", result.Gaps[0].Name)
	r.Fingerprint = "changed"
	result, err = s.Transfer(t.Context(), "cart", r)
	require.NoError(t, err)
	assert.Equal(t, "changed", result.Status)
	assert.Equal(t, int32(1), p.calls.Load())
}

func TestCartConcurrentReservation(t *testing.T) {
	p := &fakeProvider{id: "cart", workflow: CartTransfer}
	s := New(cache.NewInMemoryCache(), []Provider{p})
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { _, err := s.Transfer(t.Context(), "cart", request()); assert.NoError(t, err) })
	}
	wg.Wait()
	assert.Equal(t, int32(1), p.calls.Load())
}

func TestPreparationFailuresRemainRetryable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		empty bool
		code  string
	}{
		{"lookup", errors.New("offline"), false, "prepare_failed"},
		{"no matches", nil, true, "no_matches"},
		{"connection", &ConnectionRequired{Provider: "cart"}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &fakeProvider{id: "cart", workflow: CartTransfer}
			p.prepare = func(context.Context, Request) (Prepared, error) { return Prepared{}, tc.err }
			s := New(cache.NewInMemoryCache(), []Provider{p})
			_, err := s.Transfer(t.Context(), "cart", request())
			require.Error(t, err)
			if tc.code != "" {
				var step *StepError
				require.ErrorAs(t, err, &step)
				assert.Equal(t, tc.code, step.Code)
			} else {
				var connection *ConnectionRequired
				require.ErrorAs(t, err, &connection)
			}
			_, err = s.Load(t.Context(), "cart", request())
			require.ErrorIs(t, err, cache.ErrNotFound)
			p.prepare = nil
			_, err = s.Transfer(t.Context(), "cart", request())
			require.NoError(t, err)
			assert.Equal(t, int32(1), p.calls.Load())
		})
	}
}

func TestUncertainCartCannotResubmit(t *testing.T) {
	p := &fakeProvider{id: "cart", workflow: CartTransfer, submit: func(context.Context, Request, Prepared) (Result, error) {
		return Result{URL: "https://example.com/cart"}, errors.New("timeout")
	}}
	s := New(cache.NewInMemoryCache(), []Provider{p})
	for range 2 {
		result, err := s.Transfer(t.Context(), "cart", request())
		require.NoError(t, err)
		assert.Equal(t, "uncertain", result.Status)
		assert.Equal(t, "https://example.com/cart", result.URL)
	}
	assert.Equal(t, int32(1), p.calls.Load())
}

func TestShoppingLinksNamesMeasurementsReuseAndChange(t *testing.T) {
	p := &fakeProvider{id: "links", workflow: ShoppingListLink}
	p.submit = func(_ context.Context, r Request, _ Prepared) (Result, error) {
		assert.Empty(t, r.Items[0].ProductID)
		assert.Equal(t, "Milk", r.Items[0].Name)
		assert.Equal(t, Measurement{Quantity: 2, Unit: "cup"}, r.Items[0].Measurements[0])
		return Result{Kind: ShoppingListCreated, URL: "https://example.com/list", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	s := New(cache.NewInMemoryCache(), []Provider{p})
	r := request()
	for range 2 {
		result, err := s.Transfer(t.Context(), "links", r)
		require.NoError(t, err)
		assert.Equal(t, ShoppingListCreated, result.Kind)
		assert.Zero(t, result.Added)
		assert.Empty(t, result.Gaps)
		assert.Len(t, result.Sent, 1)
	}
	assert.Equal(t, int32(1), p.calls.Load())
	r.Fingerprint = "ignored cart fingerprint"
	_, err := s.Transfer(t.Context(), "links", r)
	require.NoError(t, err)
	assert.Equal(t, int32(1), p.calls.Load())
	r.Title = "New title"
	_, err = s.Transfer(t.Context(), "links", r)
	require.NoError(t, err)
	assert.Equal(t, int32(2), p.calls.Load())
}

func TestShoppingLinkExpiryAndFailuresRetry(t *testing.T) {
	p := &fakeProvider{id: "links", workflow: ShoppingListLink}
	p.submit = func(context.Context, Request, Prepared) (Result, error) { return Result{}, errors.New("offline") }
	s := New(cache.NewInMemoryCache(), []Provider{p})
	_, err := s.Transfer(t.Context(), "links", request())
	require.Error(t, err)
	_, err = s.Load(t.Context(), "links", request())
	require.ErrorIs(t, err, cache.ErrNotFound)
	p.submit = func(context.Context, Request, Prepared) (Result, error) {
		return Result{Kind: ShoppingListCreated, URL: "https://example.com/list", ExpiresAt: time.Now().Add(-time.Second)}, nil
	}
	for range 2 {
		_, err = s.Transfer(t.Context(), "links", request())
		require.NoError(t, err)
	}
	assert.Equal(t, int32(3), p.calls.Load())
}

func TestProviderAndUserIsolation(t *testing.T) {
	for _, workflow := range []Workflow{CartTransfer, ShoppingListLink} {
		p := &fakeProvider{id: "first", workflow: workflow}
		q := &fakeProvider{id: "second", workflow: workflow}
		s := New(cache.NewInMemoryCache(), []Provider{p, q})
		for _, user := range []string{"user", "other"} {
			for _, id := range []string{"first", "second"} {
				r := request()
				r.UserID = user
				_, err := s.Transfer(t.Context(), id, r)
				require.NoError(t, err)
			}
		}
		assert.Equal(t, int32(2), p.calls.Load())
		assert.Equal(t, int32(2), q.calls.Load())
	}
}

type failingCache struct {
	cache.Cache
	putCount int
	failAt   int
}

func (c *failingCache) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	c.putCount++
	if c.putCount == c.failAt {
		return errors.New("storage offline")
	}
	return c.Cache.Put(ctx, key, value, opts)
}

func TestCartStorageFailureSafety(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		p := &fakeProvider{id: "cart", workflow: CartTransfer}
		c := &failingCache{Cache: cache.NewInMemoryCache(), failAt: failAt}
		s := New(c, []Provider{p})
		_, err := s.Transfer(t.Context(), "cart", request())
		var step *StepError
		require.ErrorAs(t, err, &step)
		if failAt == 1 {
			assert.Equal(t, "start_failed", step.Code)
			assert.Zero(t, p.calls.Load())
		} else {
			assert.Equal(t, "confirmation_failed", step.Code)
			result, err := s.Transfer(t.Context(), "cart", request())
			require.NoError(t, err)
			assert.Equal(t, "pending", result.Status)
			assert.Equal(t, int32(1), p.calls.Load())
		}
	}
}

func TestLegacyKrogerRecords(t *testing.T) {
	c := cache.NewInMemoryCache()
	require.NoError(t, c.Put(t.Context(), "kroger_cart_transfers/user/list", `{"status":"complete","added":1,"sent":[{"id":"123","name":"Milk","quantity":"2 cups","aisle_number":"Dairy"}],"fingerprint":"snapshot"}`, cache.Unconditional()))
	p := &fakeProvider{id: "kroger", workflow: CartTransfer}
	s := New(c, []Provider{p})
	result, err := s.Transfer(t.Context(), "kroger", request())
	require.NoError(t, err)
	assert.Equal(t, CartUpdated, result.Kind)
	assert.Equal(t, "complete", result.Status)
	assert.Equal(t, "123", result.Sent[0].ProductID)
	assert.Equal(t, "2 cups", result.Sent[0].Needed)
	assert.Equal(t, "Dairy", result.Sent[0].Aisle)
	assert.Equal(t, float64(1), result.Sent[0].PackageQuantity)
	assert.Zero(t, p.calls.Load())
	reader, err := c.Get(t.Context(), "kroger_cart_transfers/user/list")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "kind")
}

func TestRequiredProviderOutput(t *testing.T) {
	for _, workflow := range []Workflow{CartTransfer, ShoppingListLink} {
		kind := CartUpdated
		if workflow == ShoppingListLink {
			kind = ShoppingListCreated
		}
		for _, tc := range []struct {
			name   string
			result Result
		}{
			{"empty URL", Result{Kind: kind}},
			{"relative URL", Result{Kind: kind, URL: "/review"}},
			{"non HTTP URL", Result{Kind: kind, URL: "javascript:alert(1)"}},
			{"missing outcome", Result{URL: "https://example.com/review"}},
			{"wrong outcome", Result{Kind: "other", URL: "https://example.com/review"}},
		} {
			t.Run(string(workflow)+"/"+tc.name, func(t *testing.T) {
				p := &fakeProvider{id: "provider", workflow: workflow, submit: func(context.Context, Request, Prepared) (Result, error) { return tc.result, nil }}
				s := New(cache.NewInMemoryCache(), []Provider{p})
				_, err := s.Transfer(t.Context(), p.ID(), request())
				var step *StepError
				require.ErrorAs(t, err, &step)
				assert.Equal(t, "submit_failed", step.Code)
				if workflow == CartTransfer {
					result, err := s.Transfer(t.Context(), p.ID(), request())
					require.NoError(t, err)
					assert.Equal(t, "uncertain", result.Status)
					assert.Equal(t, int32(1), p.calls.Load())
				} else {
					_, err := s.Load(t.Context(), p.ID(), request())
					require.ErrorIs(t, err, cache.ErrNotFound)
					p.submit = nil
					_, err = s.Transfer(t.Context(), p.ID(), request())
					require.NoError(t, err)
					assert.Equal(t, int32(2), p.calls.Load())
				}
			})
		}
	}
}

func TestOrderingRequiresAccountAndList(t *testing.T) {
	p := &fakeProvider{id: "cart", workflow: CartTransfer}
	s := New(cache.NewInMemoryCache(), []Provider{p})
	for _, r := range []Request{{Hash: "list"}, {UserID: "user"}, {UserID: " ", Hash: "list"}} {
		_, err := s.Transfer(t.Context(), "cart", r)
		require.ErrorContains(t, err, "user ID and finalized list hash")
		_, err = s.Load(t.Context(), "cart", r)
		require.ErrorContains(t, err, "user ID and finalized list hash")
	}
	assert.Zero(t, p.calls.Load())
}

func TestShoppingLinkCannotClaimCartOutcomes(t *testing.T) {
	for _, result := range []Result{
		{Kind: ShoppingListCreated, URL: "https://example.com/list", Added: 1},
		{Kind: ShoppingListCreated, URL: "https://example.com/list", Gaps: []Item{{Name: "Salt"}}},
	} {
		p := &fakeProvider{id: "links", workflow: ShoppingListLink, submit: func(context.Context, Request, Prepared) (Result, error) { return result, nil }}
		s := New(cache.NewInMemoryCache(), []Provider{p})
		_, err := s.Transfer(t.Context(), "links", request())
		require.ErrorContains(t, err, "cart additions or matching gaps")
		_, err = s.Load(t.Context(), "links", request())
		require.ErrorIs(t, err, cache.ErrNotFound)
	}
}

func TestLinkPersistenceFailureDoesNotBlockRetry(t *testing.T) {
	p := &fakeProvider{id: "links", workflow: ShoppingListLink}
	c := &failingCache{Cache: cache.NewInMemoryCache(), failAt: 1}
	s := New(c, []Provider{p})
	_, err := s.Transfer(t.Context(), "links", request())
	var step *StepError
	require.ErrorAs(t, err, &step)
	assert.Equal(t, "confirmation_failed", step.Code)
	_, err = s.Transfer(t.Context(), "links", request())
	require.NoError(t, err)
	assert.Equal(t, int32(2), p.calls.Load())
}

func TestCorruptResultBlocksSubmission(t *testing.T) {
	p := &fakeProvider{id: "cart", workflow: CartTransfer}
	c := cache.NewInMemoryCache()
	key, err := transferKey(p, request())
	require.NoError(t, err)
	require.NoError(t, c.Put(t.Context(), key, "not JSON", cache.Unconditional()))
	s := New(c, []Provider{p})
	_, err = s.Transfer(t.Context(), "cart", request())
	var step *StepError
	require.ErrorAs(t, err, &step)
	assert.Equal(t, "start_failed", step.Code)
	assert.Zero(t, p.calls.Load())
}
