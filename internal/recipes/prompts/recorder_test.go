package prompts

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
)

func TestCachePromptRecorderStoresPromptRecord(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	recorder := cacheRecorder{
		cache: cacheStore,
		now: func() time.Time {
			return time.Date(2026, time.May, 7, 12, 30, 0, 0, time.UTC)
		},
	}

	record := &ai.PromptRecord{
		ResponseID:   "resp-123",
		Model:        "gpt-test",
		Instructions: "cook well",
		Input:        []ai.PromptMessage{{Role: "user", Content: "make dinner"}},
	}

	if err := recorder.RecordPrompt(context.Background(), record); err != nil {
		t.Fatalf("RecordPrompt returned error: %v", err)
	}

	keys, err := cacheStore.List(context.Background(), CachePrefix, "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected one prompt record, got %d: %v", len(keys), keys)
	}
	if keys[0] != "resp-123" {
		t.Fatalf("unexpected prompt cache key: %s", keys[0])
	}

	reader, err := cacheStore.Get(context.Background(), CachePrefix+keys[0])
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

	var got ai.PromptRecord
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("failed to decode prompt record: %v", err)
	}
	if got.ResponseID != "resp-123" {
		t.Fatalf("expected response id to be stored, got %q", got.ResponseID)
	}
	if got.Model != "gpt-test" || got.Instructions != "cook well" {
		t.Fatalf("unexpected prompt fields: %#v", got)
	}
	if len(got.Input) != 1 || got.Input[0].Content != "make dinner" {
		t.Fatalf("unexpected input: %#v", got.Input)
	}
}
