package ai

import (
	"encoding/json"
	"testing"

	"careme/internal/locations"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
)

func TestGenerateRecipesParamsIncludesReasoningAndWebSearch(t *testing.T) {
	client := NewClient("test-key", "")
	location := &locations.Location{State: "WA"}

	params := client.generateRecipesParams(location, responses.ResponseInputParam{user("generate dinner ideas")}, "conv_123")

	body, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}

	reasoning, ok := payload["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("expected reasoning config in payload: %s", body)
	}
	if got := reasoning["effort"]; got != string(openai.ReasoningEffortHigh) {
		t.Fatalf("unexpected reasoning effort: got %v want %q", got, openai.ReasoningEffortHigh)
	}

	includes, ok := payload["include"].([]any)
	if !ok || len(includes) == 0 {
		t.Fatalf("expected include values in payload: %s", body)
	}
	if got := includes[0]; got != string(responses.ResponseIncludableWebSearchCallActionSources) {
		t.Fatalf("unexpected include value: got %v want %q", got, responses.ResponseIncludableWebSearchCallActionSources)
	}

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected one tool in payload: %s", body)
	}

	webSearch, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("expected web search tool object in payload: %s", body)
	}
	if got := webSearch["type"]; got != string(responses.WebSearchToolTypeWebSearch) {
		t.Fatalf("unexpected tool type: got %v want %q", got, responses.WebSearchToolTypeWebSearch)
	}
	if got := webSearch["search_context_size"]; got != string(responses.WebSearchToolSearchContextSizeMedium) {
		t.Fatalf("unexpected search context size: got %v want %q", got, responses.WebSearchToolSearchContextSizeMedium)
	}

	userLocation, ok := webSearch["user_location"].(map[string]any)
	if !ok {
		t.Fatalf("expected user location in tool payload: %s", body)
	}
	if got := userLocation["country"]; got != "US" {
		t.Fatalf("unexpected country: got %v want %q", got, "US")
	}
	if got := userLocation["region"]; got != "WA" {
		t.Fatalf("unexpected region: got %v want %q", got, "WA")
	}
	if got := userLocation["type"]; got != "approximate" {
		t.Fatalf("unexpected location type: got %v want %q", got, "approximate")
	}
}
