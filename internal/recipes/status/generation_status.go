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

type Payload struct {
	Message   string    `json:"message,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Error     string    `json:"error,omitempty"`
	Redirect  string    `json:"redirect,omitempty"`
}

func (p Payload) String() string {
	return p.Message
}

func (p Payload) Failed() string {
	if strings.TrimSpace(p.NewHash()) != "" {
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

// Complete(ctx context.Context, id, newHash string) error
// ID returns the stable, URL-safe identifier for a regeneration attempt.
func ID(oldHash, responseID string) string {
	h := fnv.New128a()
	_, _ = io.WriteString(h, strings.TrimSpace(oldHash))
	_, _ = io.WriteString(h, strings.TrimSpace(responseID))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (p Payload) NewHash() string {
	return p.Redirect
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
func (ss *Store) Start(ctx context.Context, hash string) error {
	return ss.save(ctx, hash, Payload{
		StartedAt: ss.now().UTC(),
	}, cache.IfNoneMatch())
}

func (ss *Store) Fail(ctx context.Context, hash string, err error) error {
	if err == nil {
		return fmt.Errorf("generation failure is required")
	}

	status, loadErr := ss.Load(ctx, hash)
	if loadErr != nil {
		return loadErr
	}
	if status.NewHash() != "" {
		return fmt.Errorf("fail generation %s: already completed", hash)
	}
	status.Error = err.Error()
	// could get overwritten by parallel update
	return ss.save(ctx, hash, status, cache.Unconditional())
}

func (ss *Store) Update(ctx context.Context, hash, message string) error {
	// this is kind of a joke since it only protects same process updates but that happens during recipe generatipm
	// should be using etags
	ss.mu.Lock()
	defer ss.mu.Unlock()

	status, err := ss.Load(ctx, hash)
	if err != nil {
		return err
	}
	status.Message = prependStatus(message, status.Message)
	return ss.save(ctx, hash, status, cache.Unconditional())
}

func (ss *Store) Complete(ctx context.Context, hash, newHash string) error {
	newHash = strings.TrimSpace(newHash)
	if newHash == "" {
		return fmt.Errorf("completed generation hash is required")
	}

	status, err := ss.Load(ctx, hash)
	if err != nil {
		return err
	}
	if status.Error != "" {
		return fmt.Errorf("complete generation %s: already failed: %s", hash, status.Error)
	}
	if completedHash := strings.TrimSpace(status.NewHash()); completedHash != "" {
		if completedHash == newHash {
			return nil
		}
		return fmt.Errorf("complete generation %s: already completed with hash %s", hash, completedHash)
	}
	status.Redirect = newHash
	return ss.save(ctx, hash, status, cache.Unconditional())
}

func (ss *Store) Load(ctx context.Context, hash string) (Payload, error) {
	statusReader, err := ss.cache.Get(ctx, generationStatusCachePrefix+hash)
	if err != nil {
		return Payload{}, fmt.Errorf("get generation status for hash %s: %w", hash, err)
	}
	defer func() {
		if err := statusReader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close generation status reader", "hash", hash, "error", err)
		}
	}()

	// buffer whole thing only for back compat below. Afetr that we can stream
	raw, err := io.ReadAll(statusReader)
	if err != nil {
		return Payload{}, fmt.Errorf("read generation status for hash %s: %w", hash, err)
	}

	var stored Payload
	if err := json.Unmarshal(raw, &stored); err == nil {
		return stored, nil
	}

	// back compat its all just strings Remove after a couple of days?
	message := strings.TrimSpace(string(raw))
	return Payload{Message: message, StartedAt: ss.now()}, nil
}

func (ss *Store) save(ctx context.Context, hash string, status Payload, opts cache.PutOptions) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return fmt.Errorf("generation hash is required")
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal generation status: %w", err)
	}
	if err := ss.cache.Put(ctx, generationStatusCachePrefix+hash, string(raw), opts); err != nil {
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
