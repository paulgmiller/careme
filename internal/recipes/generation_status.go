package recipes

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"careme/internal/cache"
)

const generationStatusCachePrefix = "generation_status/"

type statusWriter interface {
	SaveGenerationStatus(ctx context.Context, hash string, status string) error
}

type statusReader interface {
	GenerationStatusFromCache(ctx context.Context, hash string) (string, error)
}

type statusStore struct {
	cache cache.Cache
	mu    sync.Mutex
}

var (
	_ statusReader = &statusStore{}
	_ statusWriter = &statusStore{}
)

func StatusStore(c cache.Cache) *statusStore {
	return &statusStore{cache: c}
}

func (ss *statusStore) GenerationStatusFromCache(ctx context.Context, hash string) (string, error) {
	statusReader, err := ss.cache.Get(ctx, generationStatusCachePrefix+hash)
	if err != nil {
		return "", fmt.Errorf("error getting generation status for hash %s: %w", hash, err)
	}
	defer func() {
		if err := statusReader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close generation status reader", "hash", hash, "error", err)
		}
	}()
	b, err := io.ReadAll(statusReader)
	return string(b), err
}

func (ss *statusStore) SaveGenerationStatus(ctx context.Context, hash, status string) error {
	// Only needs to lock per hash, but status writes are infrequent and best effort.
	ss.mu.Lock()
	defer ss.mu.Unlock()

	key := generationStatusCachePrefix + hash
	statusReader, err := ss.cache.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			return fmt.Errorf("error getting generation status for hash %s: %w", hash, err)
		}
		statusReader = io.NopCloser(strings.NewReader(""))
	}
	defer func() {
		if err := statusReader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close generation status reader", "hash", hash, "error", err)
		}
	}()

	var sb strings.Builder
	scanner := bufio.NewScanner(io.MultiReader(strings.NewReader(strings.TrimSpace(status)+"\n"), statusReader))
	for i := 0; i < 5 && scanner.Scan(); i++ {
		sb.WriteString(scanner.Text())
		sb.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return ss.cache.Put(ctx, key, strings.TrimRight(sb.String(), "\n"), cache.Unconditional())
}
