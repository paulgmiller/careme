package parallelism

import (
	"maps"
	"sync"
)

type safeMap[K comparable, V any] struct {
	mu     sync.Mutex
	values map[K]V
}

func NewSafeMap[K comparable, V any](capacity int) *safeMap[K, V] {
	return &safeMap[K, V]{
		values: make(map[K]V, capacity),
	}
}

func (m *safeMap[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = make(map[K]V)
	}

	m.values[key] = value
}

func (m *safeMap[K, V]) Clone() map[K]V {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.values)
}
