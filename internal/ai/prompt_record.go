package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"careme/internal/cache"
)

const (
	RecipePromptCachePrefix = "recipe_prompts/"
)

type PromptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PromptRecord struct {
	CreatedAt          time.Time       `json:"created_at"`
	ResponseID         string          `json:"response_id"`
	Model              string          `json:"model"`
	Instructions       string          `json:"instructions,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	Input              []PromptMessage `json:"input"`
}

type PromptRecorder interface {
	RecordPrompt(ctx context.Context, record *PromptRecord) error
}

type cachePromptRecorder struct {
	cache cache.Cache
	now   func() time.Time
}

func NewCachePromptRecorder(c cache.Cache) PromptRecorder {
	if c == nil {
		return noopPromptRecorder{}
	}
	return cachePromptRecorder{
		cache: c,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

type noopPromptRecorder struct{}

func (noopPromptRecorder) RecordPrompt(context.Context, *PromptRecord) error {
	return nil
}

func (r cachePromptRecorder) RecordPrompt(ctx context.Context, record *PromptRecord) error {
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

func promptRecordCacheKey(record PromptRecord) string {
	return RecipePromptCachePrefix + record.ResponseID
}
