package ai

import (
	"context"
	"errors"
	"testing"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

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
