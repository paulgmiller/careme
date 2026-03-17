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
