package kroger

import (
	"io"
	"strings"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/templates"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestOrderHandler(t *testing.T, client *CartClient) *OrderHandler {
	t.Helper()
	require.NoError(t, templates.Init(&config.Config{}))
	store := cache.NewInMemoryCache()
	adapter, err := NewOrderHandler(OrderOptions{Cache: store, Client: client, TokenKey: []byte(strings.Repeat("k", 32)), Origin: "https://careme.test", Auth: auth.DefaultMock()})
	require.NoError(t, err)
	return adapter
}

func TestOrderingTokenEncryptionAndUserBinding(t *testing.T) {
	adapter := newTestOrderHandler(t, &CartClient{})
	token := cartToken{AccessToken: "secret-shopper-token", RefreshToken: "secret-refresh", ExpiresAt: time.Now().Add(time.Hour).UTC()}
	require.NoError(t, adapter.saveToken(t.Context(), "user-one", token))
	reader, err := adapter.cache.Get(t.Context(), "kroger_cart_tokens/user-one")
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.NotContains(t, string(body), token.AccessToken)
	assert.NotContains(t, string(body), token.RefreshToken)
	loaded, err := adapter.loadToken(t.Context(), "user-one")
	require.NoError(t, err)
	assert.Equal(t, token, loaded)
	require.NoError(t, adapter.cache.Put(t.Context(), "kroger_cart_tokens/user-two", string(body), cache.Unconditional()))
	_, err = adapter.loadToken(t.Context(), "user-two")
	require.Error(t, err)
}
