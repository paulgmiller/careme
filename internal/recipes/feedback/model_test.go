package feedback

import (
	"testing"
	"time"

	"careme/internal/cache"
)

func TestMarshalAndDecodeRoundTrip(t *testing.T) {
	original := Feedback{
		Cooked:    true,
		Stars:     4,
		Comment:   "Great flavor and easy cleanup.",
		UpdatedAt: time.Date(2026, 3, 17, 12, 0, 0, 0, time.UTC),
	}

	cache := cache.NewInMemoryCache()
	io := NewIO(cache)

	err := io.SaveFeedback(t.Context(), "foobar", original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	decoded, err := io.FeedbackFromCache(t.Context(), "foobar")
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if *decoded != original {
		t.Fatalf("round trip mismatch: got %#v want %#v", *decoded, original)
	}
}

func TestCookedHashes(t *testing.T) {
	cache := cache.NewInMemoryCache()
	io := NewIO(cache)

	if err := io.SaveFeedback(t.Context(), "cooked", Feedback{Cooked: true, UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("failed to save cooked feedback: %v", err)
	}
	if err := io.SaveFeedback(t.Context(), "saved", Feedback{Cooked: false, UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("failed to save uncooked feedback: %v", err)
	}

	got := io.CookedHashes(t.Context(), []string{"cooked", "saved", "missing", "", "cooked"})
	if len(got) != 1 {
		t.Fatalf("expected exactly one cooked hash, got %v", got)
	}
	if _, ok := got["cooked"]; !ok {
		t.Fatalf("expected cooked hash in result, got %v", got)
	}
}
