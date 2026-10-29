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
	AuthorizationURL(state, banner string) (string, error)
	Exchange(context.Context, string) (CartToken, error)
	Refresh(context.Context, string) (CartToken, error)
	ProductUPC(ctx context.Context, productID, locationID string) (string, error)
	Add(context.Context, string, []CartItem) error
}

// CachedOrderClient decorates UPC lookups; authorization and cart writes pass through.
type CachedOrderClient struct {
	orderClient
	cache cache.Cache
}

func NewCachedOrderClient(client orderClient, store cache.Cache) *CachedOrderClient {
	return &CachedOrderClient{orderClient: client, cache: store}
}

func (c *CachedOrderClient) ProductUPC(ctx context.Context, productID, locationID string) (string, error) {
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
	upc, err := c.orderClient.ProductUPC(ctx, productID, locationID)
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
