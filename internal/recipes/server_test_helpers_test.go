package recipes

import (
	"path/filepath"
	"testing"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/users"
)

type testServerConfig struct {
	cfg        *config.Config
	cache      cache.ListCache
	imageCache cache.Cache
	storage    *users.Storage
	generator  generator
	locServer  locServer
	clerk      auth.AuthClient
}

type testServerOption func(*testServerConfig)

func newTestServer(t testing.TB, opts ...testServerOption) *server {
	t.Helper()

	cfg := testServerConfig{
		cache:     cache.NewFileCache(filepath.Join(t.TempDir(), "cache")),
		generator: mock{},
		clerk:     auth.DefaultMock(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.imageCache == nil {
		cfg.imageCache = cfg.cache
	}
	if cfg.storage == nil {
		cfg.storage = users.NewStorage(cfg.cache)
	}

	return NewHandler(cfg.cfg, cfg.storage, cfg.generator, cfg.locServer, cfg.cache, cfg.imageCache, cfg.clerk)
}

func withTestCache(c cache.ListCache) testServerOption {
	return func(cfg *testServerConfig) {
		cfg.cache = c
	}
}

func withTestStorage(storage *users.Storage) testServerOption {
	return func(cfg *testServerConfig) {
		cfg.storage = storage
	}
}

func withTestGenerator(g generator) testServerOption {
	return func(cfg *testServerConfig) {
		cfg.generator = g
	}
}

func withTestLocationServer(ls locServer) testServerOption {
	return func(cfg *testServerConfig) {
		cfg.locServer = ls
	}
}

func withTestClerk(clerk auth.AuthClient) testServerOption {
	return func(cfg *testServerConfig) {
		cfg.clerk = clerk
	}
}
