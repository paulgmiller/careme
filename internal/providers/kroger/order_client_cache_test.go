package kroger

import (
	"context"
	"testing"

	"careme/internal/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type countingUPCClient struct {
	orderClient
	calls int
	err   error
}

func (c *countingUPCClient) ProductUPC(_ context.Context, _, _ string) (string, error) {
	c.calls++
	return "012345", c.err
}

func TestCachedOrderClientProductUPC(t *testing.T) {
	store := cache.NewInMemoryCache()
	upstream := &countingUPCClient{}
	client := NewCachedOrderClient(upstream, store)
	for range 2 {
		upc, err := client.ProductUPC(t.Context(), "product", "store")
		require.NoError(t, err)
		assert.Equal(t, "012345", upc)
	}
	assert.Equal(t, 1, upstream.calls)
	// A new wrapper reuses the persistent cache.
	_, err := NewCachedOrderClient(upstream, store).ProductUPC(t.Context(), "product", "store")
	require.NoError(t, err)
	assert.Equal(t, 1, upstream.calls)
	_, err = client.ProductUPC(t.Context(), "product", "another-store")
	require.NoError(t, err)
	assert.Equal(t, 2, upstream.calls)
	upstream.err = ErrProductUnavailable
	for range 2 {
		_, err := client.ProductUPC(t.Context(), "missing", "store")
		require.ErrorIs(t, err, ErrProductUnavailable)
	}
	assert.Equal(t, 4, upstream.calls)
}
