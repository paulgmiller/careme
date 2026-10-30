package kroger

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"careme/internal/cache"
)

// orderClient provides Kroger authorization, product resolution, and cart writes.
type orderClient interface {
	authorizationURL(state, banner string) (string, error)
	exchange(context.Context, string) (cartToken, error)
	refresh(context.Context, string) (cartToken, error)
	productUPC(ctx context.Context, productID, locationID string) (string, error)
	add(context.Context, string, []cartItem) error
}

// cachedOrderClient decorates UPC lookups; authorization and cart writes pass through.
type cachedOrderClient struct {
	orderClient
	cache cache.Cache
}

func NewCachedOrderClient(client orderClient, store cache.Cache) *cachedOrderClient {
	return &cachedOrderClient{orderClient: client, cache: store}
}

func (c *cachedOrderClient) productUPC(ctx context.Context, productID, locationID string) (string, error) {
	key := fmt.Sprintf("kroger_product_upcs/v1/%x", sha256.Sum256([]byte(locationID+"\x00"+productID)))
	reader, err := c.cache.Get(ctx, key)
	if err == nil {
		defer func() { _ = reader.Close() }()
		var upc string
		if err := json.NewDecoder(reader).Decode(&upc); err != nil {
			return "", fmt.Errorf("decode Kroger UPC cache: %w", err)
		}
		return upc, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return "", fmt.Errorf("read Kroger UPC cache: %w", err)
	}
	upc, err := c.orderClient.productUPC(ctx, productID, locationID)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(upc)
	if err != nil {
		return "", fmt.Errorf("encode Kroger UPC cache: %w", err)
	}
	if err := c.cache.Put(ctx, key, string(body), cache.Unconditional()); err != nil {
		return "", fmt.Errorf("save Kroger UPC cache: %w", err)
	}
	return upc, nil
}
