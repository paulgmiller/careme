package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"careme/internal/cache"
)

const (
	PromptRecordSchemaVersion = "recipe-prompt-v1"

	RecipePromptOperationGenerate      = "generate_recipes"
	RecipePromptOperationRegenerate    = "regenerate_recipes"
	RecipePromptOperationCritiqueRetry = "critique_retry"

	RecipePromptCachePrefix = "recipe_prompts/"
)

type PromptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type PromptRecord struct {
	SchemaVersion      string          `json:"schema_version"`
	Operation          string          `json:"operation"`
	ShoppingHash       string          `json:"shopping_hash,omitempty"`
	Model              string          `json:"model"`
	CreatedAt          time.Time       `json:"created_at"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	ResponseID         string          `json:"response_id,omitempty"`
	ResponseSchemaName string          `json:"response_schema_name,omitempty"`
	Messages           []PromptMessage `json:"messages"`
	Error              string          `json:"error,omitempty"`
}

type PromptRecorder interface {
	RecordPrompt(ctx context.Context, record *PromptRecord) error
}

type promptMetadataKey struct{}

type PromptMetadata struct {
	ShoppingHash string
	Operation    string
}

func WithPromptMetadata(ctx context.Context, metadata PromptMetadata) context.Context {
	return context.WithValue(ctx, promptMetadataKey{}, metadata)
}

func PromptMetadataFromContext(ctx context.Context) PromptMetadata {
	metadata, _ := ctx.Value(promptMetadataKey{}).(PromptMetadata)
	return metadata
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
		return nil
	}
	normalized := *record
	if normalized.SchemaVersion == "" {
		normalized.SchemaVersion = PromptRecordSchemaVersion
	}
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = r.now()
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
	shoppingHash := strings.TrimSpace(record.ShoppingHash)
	if shoppingHash == "" {
		shoppingHash = "unknown"
	}
	operation := strings.TrimSpace(record.Operation)
	if operation == "" {
		operation = "unknown"
	}
	return fmt.Sprintf("%s%s/%s_%s_%s.json",
		RecipePromptCachePrefix,
		cacheSafePathPart(shoppingHash),
		record.CreatedAt.UTC().Format("20060102T150405.000000000Z"),
		cacheSafePathPart(operation),
		randomHex(4),
	)
}

func cacheSafePathPart(part string) string {
	part = strings.TrimSpace(part)
	if part == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	return replacer.Replace(part)
}

func randomHex(bytesLen int) string {
	if bytesLen <= 0 {
		bytesLen = 4
	}
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		slog.Warn("failed to generate random prompt record suffix", "error", err)
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
