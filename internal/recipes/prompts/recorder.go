package prompts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
)

const CachePrefix = "recipe_prompts/"

type cacheRecorder struct {
	cache cache.Cache
	now   func() time.Time
}

func NewCacheRecorder(c cache.Cache) ai.PromptRecorder {
	if c == nil {
		return noopRecorder{}
	}
	return cacheRecorder{
		cache: c,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

type noopRecorder struct{}

func (noopRecorder) RecordPrompt(context.Context, *ai.PromptRecord) error {
	return nil
}

func (r cacheRecorder) RecordPrompt(ctx context.Context, record *ai.PromptRecord) error {
	if record == nil {
		return fmt.Errorf("nil prompt")
	}
	normalized := *record
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = r.now()
	}
	normalized.ResponseID = strings.TrimSpace(normalized.ResponseID)
	if normalized.ResponseID == "" {
		return fmt.Errorf("no response id")
	}

	body, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal recipe prompt record: %w", err)
	}
	if err := r.cache.Put(ctx, promptRecordCacheKey(normalized), string(body), cache.IfNoneMatch()); err != nil {
		return fmt.Errorf("write recipe prompt record: %w", err)
	}
	return nil
}

func promptRecordCacheKey(record ai.PromptRecord) string {
	return CachePrefix + record.ResponseID
}
