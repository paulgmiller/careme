package recipes

import (
	"context"
	"encoding/json"
	"errors"
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
// hash's terminal job. Single-recipe tweaks instead use recipe_regenerations/
// with an ID derived from the old recipe hash and response ID so separate
// question threads do not collide. The stores can share a generic job model in
// the future if their different IDs and completion payloads are made explicit.
type generationState string

const (
	generationRunning  generationState = "running"
	generationComplete generationState = "complete"
	generationFailed   generationState = "failed"
)

type generationStatus struct {
	State     generationState `json:"state"`
	Message   string          `json:"message,omitempty"`
	StartedAt time.Time       `json:"started_at"`
}

func (s generationStatus) valid() bool {
	switch s.State {
	case generationRunning, generationComplete, generationFailed:
		return true
	default:
		return false
	}
}

type statusWriter interface {
	SaveGenerationStatus(ctx context.Context, hash string, status string) error
}

type statusStore struct {
	cache cache.Cache
	mu    sync.Mutex
	now   func() time.Time
}

var _ statusWriter = &statusStore{}

func StatusStore(c cache.Cache) *statusStore {
	return &statusStore{cache: c, now: time.Now}
}

func (ss *statusStore) Start(ctx context.Context, hash string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	return ss.save(ctx, hash, generationStatus{
		State:     generationRunning,
		StartedAt: ss.now().UTC(),
	})
}

func (ss *statusStore) Complete(ctx context.Context, hash string) error {
	return ss.setState(ctx, hash, generationComplete, "")
}

func (ss *statusStore) Fail(ctx context.Context, hash string, err error) error {
	if err == nil {
		return fmt.Errorf("generation failure is required")
	}
	return ss.setState(ctx, hash, generationFailed, "Something went wrong: "+err.Error())
}

func (ss *statusStore) setState(ctx context.Context, hash string, state generationState, message string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	status, err := ss.load(ctx, hash)
	if err != nil {
		return err
	}
	status.State = state
	if strings.TrimSpace(message) != "" {
		status.Message = prependStatus(message, status.Message)
	}
	return ss.save(ctx, hash, status)
}

func (ss *statusStore) GenerationStatusFromCache(ctx context.Context, hash string) (generationStatus, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	return ss.load(ctx, hash)
}

func (ss *statusStore) SaveGenerationStatus(ctx context.Context, hash, message string) error {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	status, err := ss.load(ctx, hash)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			return err
		}
		status = generationStatus{
			State:     generationRunning,
			StartedAt: ss.now().UTC(),
		}
	}
	if status.StartedAt.IsZero() {
		status.StartedAt = ss.now().UTC()
	}
	status.Message = prependStatus(message, status.Message)
	return ss.save(ctx, hash, status)
}

func (ss *statusStore) load(ctx context.Context, hash string) (generationStatus, error) {
	statusReader, err := ss.cache.Get(ctx, generationStatusCachePrefix+hash)
	if err != nil {
		return generationStatus{}, fmt.Errorf("get generation status for hash %s: %w", hash, err)
	}
	defer func() {
		if err := statusReader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close generation status reader", "hash", hash, "error", err)
		}
	}()

	raw, err := io.ReadAll(statusReader)
	if err != nil {
		return generationStatus{}, fmt.Errorf("read generation status for hash %s: %w", hash, err)
	}

	var status generationStatus
	if err := json.Unmarshal(raw, &status); err == nil {
		if !status.valid() {
			return generationStatus{}, fmt.Errorf("invalid generation status state %q", status.State)
		}
		return status, nil
	}

	message := strings.TrimSpace(string(raw))
	state := generationRunning
	if strings.HasPrefix(message, "Something went wrong:") {
		state = generationFailed
	}
	return generationStatus{State: state, Message: message}, nil
}

func (ss *statusStore) save(ctx context.Context, hash string, status generationStatus) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return fmt.Errorf("generation hash is required")
	}
	if !status.valid() {
		return fmt.Errorf("invalid generation status state %q", status.State)
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
