package recipes

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"careme/internal/cache"
)

const generationStatusCachePrefix = "generation_status/"

// Full-plan generation uses the shopping-list hash as its job ID because one
// cached result exists per parameter hash; an explicit retry replaces that
// hash's previous attempt. Single-recipe tweaks instead use recipe_regenerations/
// with an ID derived from the old recipe hash and response ID so separate
// question threads do not collide. The stores can share a generic job model in
// the future if their different IDs and completion payloads are made explicit.
type generationStatus struct {
	Message   string    `json:"message,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Error     string    `json:"error,omitempty"`
}

type statusStore struct {
	mu    sync.Mutex
	cache cache.Cache
	now   func() time.Time
}

func StatusStore(c cache.Cache) *statusStore {
	return &statusStore{cache: c, now: time.Now}
}

// Start  creates or resets an existing
func (ss *statusStore) Start(ctx context.Context, hash string) error {
	return ss.save(ctx, hash, generationStatus{
		StartedAt: ss.now().UTC(),
	})
}

func (ss *statusStore) Fail(ctx context.Context, hash string, err error) error {
	if err == nil {
		return fmt.Errorf("generation failure is required")
	}

	status, loadErr := ss.Load(ctx, hash)
	if loadErr != nil {
		return loadErr
	}
	status.Error = err.Error()
	// could get overwritten by parallel update
	return ss.save(ctx, hash, status)
}

func (ss *statusStore) Update(ctx context.Context, hash, message string) error {
	// this is kind of a joke since it only protects same process updates but that happens during recipe generatipm
	// should be using etags
	ss.mu.Lock()
	defer ss.mu.Unlock()

	status, err := ss.Load(ctx, hash)
	if err != nil {
		return err
	}
	status.Message = prependStatus(message, status.Message)
	return ss.save(ctx, hash, status)
}

func (ss *statusStore) Load(ctx context.Context, hash string) (generationStatus, error) {
	statusReader, err := ss.cache.Get(ctx, generationStatusCachePrefix+hash)
	if err != nil {
		return generationStatus{}, fmt.Errorf("get generation status for hash %s: %w", hash, err)
	}
	defer func() {
		if err := statusReader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close generation status reader", "hash", hash, "error", err)
		}
	}()

	// buffer whole thing only for back compat below. Afetr that we can stream
	raw, err := io.ReadAll(statusReader)
	if err != nil {
		return generationStatus{}, fmt.Errorf("read generation status for hash %s: %w", hash, err)
	}

	var stored generationStatus
	if err := json.Unmarshal(raw, &stored); err == nil {
		return stored, nil
	}

	// back compat its all just strings Remove after a couple of days?
	message := strings.TrimSpace(string(raw))
	return generationStatus{Message: message, StartedAt: ss.now()}, nil
}

func (ss *statusStore) save(ctx context.Context, hash string, status generationStatus) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return fmt.Errorf("generation hash is required")
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal generation status: %w", err)
	}
	if err := ss.cache.Put(ctx, generationStatusCachePrefix+hash, string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("save generation status for hash %s: %w", hash, err)
	}
	return nil
}

func prependStatus(message, previous string) string {
	lines := strings.Split(strings.TrimSpace(message), "\n")
	if strings.TrimSpace(message) == "" {
		lines = nil
	}
	if previous = strings.TrimSpace(previous); previous != "" {
		lines = append(lines, strings.Split(previous, "\n")...)
	}
	if len(lines) > 5 {
		lines = lines[:5]
	}
	return strings.Join(lines, "\n")
}
