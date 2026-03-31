package cache

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"sync"
)

// InMemoryCache stores cache entries in process memory.
type InMemoryCache struct {
	mu   sync.RWMutex
	data map[string][]byte
}

var (
	_ Cache     = (*InMemoryCache)(nil)
	_ ListCache = (*InMemoryCache)(nil)
)

func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		data: make(map[string][]byte),
	}
}

func (c *InMemoryCache) Get(_ context.Context, key string) (io.ReadCloser, error) {
	c.mu.RLock()
	value, ok := c.data[key]
	c.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func (c *InMemoryCache) Exists(_ context.Context, key string) (bool, error) {
	c.mu.RLock()
	_, ok := c.data[key]
	c.mu.RUnlock()
	return ok, nil
}

func (c *InMemoryCache) Put(ctx context.Context, key, value string, opts PutOptions) error {
	return c.PutReader(ctx, key, strings.NewReader(value), opts)
}

func (c *InMemoryCache) PutReader(_ context.Context, key string, reader io.Reader, opts PutOptions) error {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if opts.Condition == PutIfNoneMatch {
		if _, exists := c.data[key]; exists {
			return ErrAlreadyExists
		}
	}

	c.data[key] = buf.Bytes()
	return nil
}

func (c *InMemoryCache) List(_ context.Context, prefix string, _ string) ([]string, error) {
	c.mu.RLock()
	keys := make([]string, 0)
	for key := range c.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, strings.TrimPrefix(key, prefix))
		}
	}
	c.mu.RUnlock()

	sort.Strings(keys)
	return keys, nil
}
