package status

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"careme/internal/cache"
)

const (
	generationStatusCachePrefix = "generation_status/"
	recipeGenerationTimeout     = 10 * time.Minute
)

type Status struct {
	Message  string
	Failed   string
	Redirect string
}

type payload struct {
	Message   string    `json:"message,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Error     string    `json:"error,omitempty"`
	Redirect  string    `json:"redirect,omitempty"`
}

func (p payload) failed() string {
	if strings.TrimSpace(p.Redirect) != "" {
		return ""
	}
	if p.Error != "" {
		return p.Error
	}
	if time.Since(p.StartedAt) >= recipeGenerationTimeout {
		return "Recipe generation timed out."
	}

	return ""
}

// ID returns the stable, URL-safe identifier for a regeneration attempt.
func ID(oldHash, responseID string) string {
	h := fnv.New128a()
	_, _ = io.WriteString(h, strings.TrimSpace(oldHash))
	_, _ = io.WriteString(h, strings.TrimSpace(responseID))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// IsValidID reports whether id is a canonical regeneration identifier.
func IsValidID(id string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	return err == nil && len(raw) == 16 && base64.RawURLEncoding.EncodeToString(raw) == id
}

type Store struct {
	mu    sync.Mutex
	cache cache.Cache
	now   func() time.Time
}

func NewStore(c cache.Cache) *Store {
	return &Store{cache: c, now: time.Now}
}

// Start  creates or resets an existing
// TODO take a cache option so we can do this oon not exists.
func (ss *Store) Start(ctx context.Context, hash string) error {
	return ss.save(ctx, hash, payload{
		StartedAt: ss.now().UTC(),
	})
}

func (ss *Store) Fail(ctx context.Context, hash string, err error) error {
	if err == nil {
		return fmt.Errorf("generation failure is required")
	}

	status, loadErr := ss.load(ctx, hash)
	if loadErr != nil {
		return loadErr
	}
	if status.Redirect != "" {
		return fmt.Errorf("fail generation %s: already completed", hash)
	}
	status.Error = err.Error()
	// could get overwritten by parallel update
	return ss.save(ctx, hash, status)
}

func (ss *Store) Update(ctx context.Context, hash, message string) error {
	// this is kind of a joke since it only protects same process updates but that happens during recipe generatipm
	// should be using etags
	ss.mu.Lock()
	defer ss.mu.Unlock()

	status, err := ss.load(ctx, hash)
	if err != nil {
		return err
	}
	status.Message = prependStatus(message, status.Message)
	return ss.save(ctx, hash, status)
}

func (ss *Store) Complete(ctx context.Context, hash, newHash string) error {
	newHash = strings.TrimSpace(newHash)
	if newHash == "" {
		return fmt.Errorf("completed generation hash is required")
	}

	status, err := ss.load(ctx, hash)
	if err != nil {
		return err
	}
	if status.Error != "" {
		return fmt.Errorf("complete generation %s: already failed: %s", hash, status.Error)
	}
	if completedHash := strings.TrimSpace(status.Redirect); completedHash != "" {
		if completedHash == newHash {
			return nil
		}
		return fmt.Errorf("complete generation %s: already completed with hash %s", hash, completedHash)
	}
	status.Redirect = newHash
	return ss.save(ctx, hash, status)
}

func (ss *Store) Load(ctx context.Context, hash string) (Status, error) {
	stored, err := ss.load(ctx, hash)
	if err != nil {
		return Status{}, err
	}
	return Status{
		Message:  stored.Message,
		Failed:   stored.failed(),
		Redirect: stored.Redirect,
	}, nil
}

func (ss *Store) load(ctx context.Context, hash string) (payload, error) {
	statusReader, err := ss.cache.Get(ctx, generationStatusCachePrefix+hash)
	if err != nil {
		return payload{}, fmt.Errorf("get generation status for hash %s: %w", hash, err)
	}
	defer func() {
		if err := statusReader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close generation status reader", "hash", hash, "error", err)
		}
	}()

	var stored payload
	if err := json.NewDecoder(statusReader).Decode(&stored); err != nil {
		return payload{}, err
	}
	return stored, nil
}

func (ss *Store) save(ctx context.Context, hash string, status payload) error {
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
