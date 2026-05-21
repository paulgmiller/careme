package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

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

func schemaRequired(t *testing.T, schema map[string]any) []string {
	t.Helper()
	raw, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("expected required array, got %#v", schema["required"])
	}
	required := make([]string, 0, len(raw))
	for _, value := range raw {
		field, ok := value.(string)
		if !ok {
			t.Fatalf("expected required field string, got %#v", value)
		}
		required = append(required, field)
	}
	return required
}

func schemaProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties object, got %#v", schema["properties"])
	}
	return properties
}

func schemaObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected schema object, got %#v", value)
	}
	return object
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(body)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
