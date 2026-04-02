package watchdog

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOncePerDo(t *testing.T) {
	t.Parallel()

	dog := &stubWatchdog{}
	guard := NewOncePer(time.Hour, dog)

	if err := guard.Watchdog(context.Background()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got, want := dog.calls, 1; got != want {
		t.Fatalf("calls after first run = %d, want %d", got, want)
	}

	err := guard.Watchdog(context.Background())
	if !errors.Is(err, errTooSoon) {
		t.Fatalf("second call error = %v, want %v", err, errTooSoon)
	}
	if got, want := dog.calls, 1; got != want {
		t.Fatalf("calls after blocked run = %d, want %d", got, want)
	}

	guard.last = time.Now().Add(-2 * time.Hour)
	if err := guard.Watchdog(context.Background()); err != nil {
		t.Fatalf("third call after period: %v", err)
	}
	if got, want := dog.calls, 2; got != want {
		t.Fatalf("calls after third run = %d, want %d", got, want)
	}
}
