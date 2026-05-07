package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"careme/internal/cache"
)

func TestCachePromptRecorderStoresPromptRecord(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	recorder := cachePromptRecorder{
		cache: cacheStore,
		now: func() time.Time {
			return time.Date(2026, time.May, 7, 12, 30, 0, 0, time.UTC)
		},
	}

	record := &PromptRecord{
		Operation:          RecipePromptOperationGenerate,
		ShoppingHash:       "shopping/hash",
		Model:              "gpt-test",
		ResponseID:         "resp-123",
		ResponseSchemaName: "recipes",
		Messages: []PromptMessage{
			{Role: "system", Content: "cook well"},
			{Role: "user", Content: "make dinner"},
		},
	}

	if err := recorder.RecordPrompt(context.Background(), record); err != nil {
		t.Fatalf("RecordPrompt returned error: %v", err)
	}

	keys, err := cacheStore.List(context.Background(), RecipePromptCachePrefix, "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected one prompt record, got %d: %v", len(keys), keys)
	}
	if !strings.HasPrefix(keys[0], "shopping_hash/20260507T123000.000000000Z_generate_recipes_") {
		t.Fatalf("unexpected prompt cache key: %s", keys[0])
	}

	reader, err := cacheStore.Get(context.Background(), RecipePromptCachePrefix+keys[0])
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Fatalf("failed to close reader: %v", err)
		}
	}()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}

	var got PromptRecord
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode prompt record: %v", err)
	}
	if got.SchemaVersion != PromptRecordSchemaVersion {
		t.Fatalf("expected schema version %q, got %q", PromptRecordSchemaVersion, got.SchemaVersion)
	}
	if got.ShoppingHash != "shopping/hash" {
		t.Fatalf("expected original shopping hash to be stored, got %q", got.ShoppingHash)
	}
	if got.ResponseID != "resp-123" {
		t.Fatalf("expected response id to be stored, got %q", got.ResponseID)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.Messages[1].Content != "make dinner" {
		t.Fatalf("unexpected messages: %#v", got.Messages)
	}
}

type capturePromptRecorder struct {
	record *PromptRecord
	err    error
}

func (r *capturePromptRecorder) RecordPrompt(_ context.Context, record *PromptRecord) error {
	clone := *record
	clone.Messages = append([]PromptMessage(nil), record.Messages...)
	r.record = &clone
	return r.err
}

func TestRecordRecipePromptUsesContextMetadata(t *testing.T) {
	recorder := &capturePromptRecorder{}
	c := &client{
		model:          "gpt-test",
		promptRecorder: recorder,
	}
	ctx := WithPromptMetadata(context.Background(), PromptMetadata{
		ShoppingHash: "shopping-hash",
		Operation:    RecipePromptOperationCritiqueRetry,
	})

	c.recordRecipePrompt(ctx, "resp-before", "resp-after", []PromptMessage{{Role: "user", Content: "try again"}}, nil)

	if recorder.record == nil {
		t.Fatal("expected prompt record")
	}
	if recorder.record.Operation != RecipePromptOperationCritiqueRetry {
		t.Fatalf("expected critique retry operation, got %q", recorder.record.Operation)
	}
	if recorder.record.ShoppingHash != "shopping-hash" {
		t.Fatalf("expected shopping hash, got %q", recorder.record.ShoppingHash)
	}
	if recorder.record.PreviousResponseID != "resp-before" || recorder.record.ResponseID != "resp-after" {
		t.Fatalf("unexpected response ids: %#v", recorder.record)
	}
}

func TestRecordRecipePromptIgnoresRecorderErrors(t *testing.T) {
	recorder := &capturePromptRecorder{err: errors.New("cache down")}
	c := &client{
		model:          "gpt-test",
		promptRecorder: recorder,
	}

	c.recordRecipePrompt(context.Background(), "", "", []PromptMessage{{Role: "user", Content: "make dinner"}}, errors.New("api failed"))

	if recorder.record == nil {
		t.Fatal("expected prompt record even when recorder returns an error")
	}
	if recorder.record.Operation != RecipePromptOperationGenerate {
		t.Fatalf("expected generate operation fallback, got %q", recorder.record.Operation)
	}
	if recorder.record.Error != "api failed" {
		t.Fatalf("expected API error to be stored, got %q", recorder.record.Error)
	}
}
