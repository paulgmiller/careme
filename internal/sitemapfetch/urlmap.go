package sitemapfetch

import (
	"careme/internal/cache"
	"context"
	"encoding/json"
	"fmt"
)

func SaveURLMap(ctx context.Context, c cache.Cache, cacheKey string, urlMap map[string]string) error {
	raw, err := json.Marshal(urlMap)
	if err != nil {
		return fmt.Errorf("marshal url map: %w", err)
	}
	if err := c.Put(ctx, cacheKey, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("write url map cache: %w", err)
	}
	return nil
}

func LoadURLMap(ctx context.Context, c cache.Cache, cacheKey string) (map[string]string, error) {
	reader, err := c.Get(ctx, cacheKey)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()

	var urlMap map[string]string
	if err := json.NewDecoder(reader).Decode(&urlMap); err != nil {
		return nil, fmt.Errorf("decode url map cache: %w", err)
	}
	return urlMap, nil
}
