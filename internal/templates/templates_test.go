package templates

import (
	"context"
	"strings"
	"testing"

	"careme/internal/logsetup"
)

func TestClarityScriptIncludesSessionID(t *testing.T) {
	prev := Clarityproject
	t.Cleanup(func() {
		Clarityproject = prev
	})
	Clarityproject = "proj-123"

	script := string(ClarityScript(logsetup.WithSessionID(context.Background(), "sess-123")))
	if !strings.Contains(script, `www.clarity.ms/tag/`) {
		t.Fatal("expected clarity script url")
	}
	if !strings.Contains(script, `window.clarity("identify", "sess-123", "sess-123")`) {
		t.Fatalf("expected identify call in script, got %q", script)
	}
}

func TestClarityScriptOmitsIdentifyWhenSessionIDEmpty(t *testing.T) {
	prev := Clarityproject
	t.Cleanup(func() {
		Clarityproject = prev
	})
	Clarityproject = "proj-123"

	script := string(ClarityScript(context.Background()))
	if strings.Contains(script, `window.clarity("identify"`) {
		t.Fatalf("did not expect identify call in script, got %q", script)
	}
}
