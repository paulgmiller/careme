package recipes

import (
	"context"
	"fmt"
	"io"
	"log/slog"

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
}

var (
	_ statusReader = &statusStore{}
	_ statusWriter = &statusStore{}
)

func StatusStore(c cache.Cache) *statusStore {
	return &statusStore{c}
}

func (ss statusStore) GenerationStatusFromCache(ctx context.Context, hash string) (string, error) {
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

func (ss statusStore) SaveGenerationStatus(ctx context.Context, hash, status string) error {
	return ss.cache.Put(ctx, generationStatusCachePrefix+hash, status, cache.Unconditional())
}
