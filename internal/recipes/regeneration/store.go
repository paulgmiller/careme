// regeneration just tracks the regeneration of a recipe based on questions when user asks to tweak it.
// could also just be a general job packagel.
package regeneration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"strings"
	"time"

	"careme/internal/cache"
)

const cachePrefix = "recipe_regenerations/"

type job struct {
	NewHash   string    `json:"new_hash,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store persists and reads the progress of a single-recipe regeneration.
type Store struct {
	cache   cache.Cache
	timeout time.Duration
}

func NewStore(c cache.Cache, timeout time.Duration) *Store {
	if c == nil {
		panic("cache must not be nil")
	}
	if timeout <= 0 {
		panic("timeout must be positive")
	}
	return &Store{cache: c, timeout: timeout}
}

// ID returns the stable, URL-safe identifier for a regeneration attempt.
func ID(oldHash, responseID string) string {
	h := fnv.New128a()
	_, _ = io.WriteString(h, strings.TrimSpace(oldHash))
	_, _ = io.WriteString(h, strings.TrimSpace(responseID))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// Start records a new regeneration without replacing an existing attempt.
func (s *Store) Start(ctx context.Context, id string) error {
	return s.save(ctx, id, job{UpdatedAt: time.Now().UTC()}, cache.IfNoneMatch())
}

// Restart resets the timeout for an existing regeneration attempt.
func (s *Store) Restart(ctx context.Context, id string) error {
	return s.save(ctx, id, job{UpdatedAt: time.Now().UTC()}, cache.Unconditional())
}

// Complete records the hash produced by a regeneration attempt.
func (s *Store) Complete(ctx context.Context, id, newHash string) error {
	if strings.TrimSpace(newHash) == "" {
		return fmt.Errorf("new recipe hash is required")
	}
	return s.save(ctx, id, job{
		NewHash:   newHash,
		UpdatedAt: time.Now().UTC(),
	}, cache.Unconditional())
}

// Load returns the generated recipe hash when complete and whether an in-progress
// regeneration has timed out. A running regeneration returns an empty hash.
func (s *Store) Load(ctx context.Context, id string) (newHash string, timedOut bool, err error) {
	id = strings.TrimSpace(id)
	if !validID(id) {
		return "", false, cache.ErrNotFound
	}

	r, err := s.cache.Get(ctx, cacheKey(id))
	if err != nil {
		return "", false, err
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "failed to close recipe regeneration", "id", id, "error", closeErr)
		}
	}()

	var stored job
	if err := json.NewDecoder(r).Decode(&stored); err != nil {
		return "", false, fmt.Errorf("decode recipe regeneration: %w", err)
	}

	if strings.TrimSpace(stored.NewHash) != "" {
		return stored.NewHash, false, nil
	}
	return "", time.Since(stored.UpdatedAt) >= s.timeout, nil
}

func (s *Store) save(ctx context.Context, id string, stored job, opts cache.PutOptions) error {
	if !validID(id) {
		return fmt.Errorf("invalid recipe regeneration id")
	}

	raw, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("marshal recipe regeneration: %w", err)
	}
	if err := s.cache.Put(ctx, cacheKey(id), string(raw), opts); err != nil {
		return fmt.Errorf("save recipe regeneration: %w", err)
	}
	return nil
}

func cacheKey(id string) string {
	return cachePrefix + strings.TrimSpace(id)
}

func validID(id string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(raw) == 16 && base64.RawURLEncoding.EncodeToString(raw) == id
}
