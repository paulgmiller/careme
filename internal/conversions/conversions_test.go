package conversions

import (
	"strings"
	"testing"

	"careme/internal/cache"
)

func TestRecordOnceOnlyRecordsFirstKey(t *testing.T) {
	recorder := NewRecorder(cache.NewInMemoryCache())

	if !recorder.RecordOnce(t.Context(), EventSignIn, "user-1") {
		t.Fatal("first RecordOnce should record")
	}
	if recorder.RecordOnce(t.Context(), EventSignIn, "user-1") {
		t.Fatal("second RecordOnce with same key should not record")
	}
	if !recorder.RecordOnce(t.Context(), EventSignIn, "user-2") {
		t.Fatal("different key should record")
	}
}

func TestConsumeBrowserPendingOnlyReturnsTrueOnce(t *testing.T) {
	recorder := NewRecorder(cache.NewInMemoryCache())

	recorder.MarkBrowserPending(t.Context(), EventRecipeGeneration, "hash-1")
	if !recorder.ConsumeBrowserPending(t.Context(), EventRecipeGeneration, "hash-1") {
		t.Fatal("first pending browser conversion should be consumed")
	}
	if recorder.ConsumeBrowserPending(t.Context(), EventRecipeGeneration, "hash-1") {
		t.Fatal("pending browser conversion should only be consumed once")
	}
}

func TestBrowserConfigUsesGoogleLabelsAndLegacySignInFallback(t *testing.T) {
	t.Setenv(envGoogleLabelsJSON, `{"recipe_save":"save-label"}`)

	cfg := BrowserConfigFromEnv("AW-123", "signin-label")

	if got, want := cfg.GoogleConversionTag(EventRecipeSave), "AW-123/save-label"; got != want {
		t.Fatalf("recipe_save tag = %q, want %q", got, want)
	}
	if got, want := cfg.GoogleConversionTag(EventSignIn), "AW-123/signin-label"; got != want {
		t.Fatalf("sign_in tag = %q, want %q", got, want)
	}

	script := string(cfg.Script(EventRecipeSave))
	if !strings.Contains(script, `"recipe_save":"AW-123/save-label"`) {
		t.Fatalf("script should include recipe save tag, got %s", script)
	}
	if !strings.Contains(script, `const pendingEvent = "recipe_save";`) {
		t.Fatalf("script should include pending event, got %s", script)
	}
}
