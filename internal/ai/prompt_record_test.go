package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"careme/internal/cache"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
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
		ResponseID:   "resp-123",
		Model:        "gpt-test",
		Instructions: "cook well",
		Input:        []PromptMessage{userPromptMessage("make dinner")},
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
	if keys[0] != "resp-123" {
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

type capturePromptRecorder struct {
	record *PromptRecord
	err    error
}

func (r *capturePromptRecorder) RecordPrompt(_ context.Context, record *PromptRecord) error {
	clone := *record
	clone.Input = append([]PromptMessage(nil), record.Input...)
	r.record = &clone
	return r.err
}

func TestRecordRecipePromptStoresResponseParams(t *testing.T) {
	recorder := &capturePromptRecorder{}
	c := &client{
		model:          "gpt-test",
		promptRecorder: recorder,
	}
	ctx := context.Background()
	params := responses.ResponseNewParams{
		Model:              "gpt-test",
		Instructions:       openai.String("cook well"),
		PreviousResponseID: openai.String("resp-before"),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: []responses.ResponseInputItemUnionParam{user("try again")},
		},
		Store: openai.Bool(true),
	}

	c.recordRecipePrompt(ctx, "resp-after", params, []PromptMessage{userPromptMessage("try again")})

	if recorder.record == nil {
		t.Fatal("expected prompt record")
	}
	if recorder.record.ResponseID != "resp-after" {
		t.Fatalf("unexpected response id: %#v", recorder.record)
	}
	if recorder.record.Model != "gpt-test" || recorder.record.PreviousResponseID != "resp-before" {
		t.Fatalf("unexpected prompt record: %#v", recorder.record)
	}
	if recorder.record.Instructions != "cook well" {
		t.Fatalf("unexpected instructions: %#v", recorder.record.Instructions)
	}
	if len(recorder.record.Input) != 1 || recorder.record.Input[0].Content != "try again" {
		t.Fatalf("unexpected input: %#v", recorder.record.Input)
	}
}

func TestRecordRecipePromptIgnoresRecorderErrors(t *testing.T) {
	recorder := &capturePromptRecorder{err: errors.New("cache down")}
	c := &client{
		model:          "gpt-test",
		promptRecorder: recorder,
	}

	c.recordRecipePrompt(context.Background(), "resp-123", responses.ResponseNewParams{Model: "gpt-test"}, []PromptMessage{userPromptMessage("make dinner")})

	if recorder.record == nil {
		t.Fatal("expected prompt record")
	}
}

func TestRecordRecipePromptSkipsMissingResponseID(t *testing.T) {
	recorder := &capturePromptRecorder{}
	c := &client{
		model:          "gpt-test",
		promptRecorder: recorder,
	}

	c.recordRecipePrompt(context.Background(), "", responses.ResponseNewParams{Model: "gpt-test"}, []PromptMessage{userPromptMessage("make dinner")})

	if recorder.record != nil {
		t.Fatalf("expected missing response ID to skip prompt record, got %#v", recorder.record)
	}
}
