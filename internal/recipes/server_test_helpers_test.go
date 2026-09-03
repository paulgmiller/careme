package recipes

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/status"
	"careme/internal/users"
)

type testServerConfig struct {
	cfg        *config.Config
	cache      cache.ListCache
	imageCache cache.Cache
	storage    *users.Storage
	generator  generator
	imagegen   ImageGen
	locServer  locServer
	clerk      auth.AuthClient
	statuses   statusStore
}

type testServerOption func(*testServerConfig)

func newTestServer(t testing.TB, opts ...testServerOption) *server {
	t.Helper()

	cfg := testServerConfig{
		cache: cache.NewFileCache(filepath.Join(t.TempDir(), "cache")),
		clerk: auth.DefaultMock(),
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
	if cfg.generator == nil {
		cfg.generator = NewMockGenerator(IO(cfg.cache), critique.NewMock(cfg.cache))
	}

	if cfg.imagegen == nil {
		cfg.imagegen = mock{}
	}

	s := NewHandler(cfg.cfg, cfg.storage, cfg.generator, cfg.locServer, cfg.cache, cfg.imageCache, cfg.clerk, cfg.imagegen)
	if cfg.statuses != nil {
		s.generationStatuses = cfg.statuses
	}
	return s
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

func withImageGenerator(g ImageGen) testServerOption {
	return func(cfg *testServerConfig) {
		cfg.imagegen = g
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

func withTestStatusStore(statuses statusStore) testServerOption {
	return func(cfg *testServerConfig) {
		cfg.statuses = statuses
	}
}

type fakeStatusStore struct {
	mu       sync.Mutex
	statuses map[string]status.Status
}

func newFakeStatusStore() *fakeStatusStore {
	return &fakeStatusStore{statuses: make(map[string]status.Status)}
}

func (s *fakeStatusStore) Start(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[hash] = status.Status{}
	return nil
}

func (s *fakeStatusStore) Fail(_ context.Context, hash string, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := s.statuses[hash]
	payload.Failed = err.Error()
	s.statuses[hash] = payload
	return nil
}

func (s *fakeStatusStore) Load(_ context.Context, hash string) (status.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.statuses[hash]
	if !ok {
		return status.Status{}, cache.ErrNotFound
	}
	return payload, nil
}

func (s *fakeStatusStore) Complete(_ context.Context, id, newHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := s.statuses[id]
	payload.Redirect = newHash
	s.statuses[id] = payload
	return nil
}

func (s *fakeStatusStore) setProgress(hash, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[hash] = status.Status{Message: message}
}

func (s *fakeStatusStore) failure(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if failure := s.statuses[hash].Failed; failure != "" {
		return errors.New(failure)
	}
	return nil
}

func (s *fakeStatusStore) started(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.statuses[hash]
	return ok
}
