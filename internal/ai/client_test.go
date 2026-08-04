package ai

import (
	"testing"
	"time"

	"careme/internal/logsetup"
)

func TestNewClientUsesGPT56FamilyByRole(t *testing.T) {
	client := NewClient("test-key", "ignored", nil, &capturePromptRecorder{})

	if client.model != gpt56Sol {
		t.Fatalf("expected primary recipe model to be %q, got %q", gpt56Sol, client.model)
	}
	if client.wineModel != gpt56Luna {
		t.Fatalf("expected wine model to use low-cost Luna path, got %q", client.wineModel)
	}
}

func TestRecipePromptCacheKeyUsesStoreAndDateAcrossUsers(t *testing.T) {
	date := time.Date(2026, time.August, 4, 15, 0, 0, 0, time.FixedZone("Pacific", -7*60*60))
	first := logsetup.WithUserID(t.Context(), "user-one")
	first = logsetup.WithSessionID(first, "session-one")
	second := logsetup.WithUserID(t.Context(), "user-two")
	second = logsetup.WithSessionID(second, "session-two")

	firstRef := ResponseRef{PromptCacheKey: storeDayPromptCacheKey("store-123", date.Format("2006-01-02"))}
	secondRef := ResponseRef{PromptCacheKey: storeDayPromptCacheKey("store-123", date.Format("2006-01-02"))}

	if got, want := responsePromptCacheKey(first, firstRef), responsePromptCacheKey(second, secondRef); got != want {
		t.Fatalf("expected users at the same store and date to share a cache key, got %q and %q", got, want)
	}
	nextDate := storeDayPromptCacheKey("store-123", date.AddDate(0, 0, 1).Format("2006-01-02"))
	if firstRef.PromptCacheKey == nextDate {
		t.Fatal("expected a different date to use a different cache key")
	}
}
