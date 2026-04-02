package recipes

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"careme/internal/cache"
)

func TestOncePerDo_SharedCacheRunsOnceAcrossInstances(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	base := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)

	guard1, err := NewOncePer(cacheStore, "staples-ready")
	if err != nil {
		t.Fatalf("new onceper: %v", err)
	}
	guard1.now = func() time.Time { return base }
	guard1.claimTTL = time.Second

	guard2, err := NewOncePer(cacheStore, "staples-ready")
	if err != nil {
		t.Fatalf("new onceper: %v", err)
	}
	guard2.now = func() time.Time { return base }
	guard2.claimTTL = time.Second

	var runs atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	secondResult := make(chan error, 1)
	firstResult := make(chan error, 1)

	go func() {
		firstResult <- guard1.Do(t.Context(), time.Hour, func() error {
			runs.Add(1)
			started <- struct{}{}
			<-release
			return nil
		})
	}()

	<-started

	go func() {
		secondResult <- guard2.Do(t.Context(), time.Hour, func() error {
			runs.Add(1)
			return errors.New("second replica should not run")
		})
	}()

	if err := <-secondResult; !errors.Is(err, ErrOncePerInProgress) {
		t.Fatalf("expected in-progress error, got %v", err)
	}
	close(release)

	if err := <-firstResult; err != nil {
		t.Fatalf("unexpected onceper error: %v", err)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("expected one shared execution, got %d", got)
	}
}

func TestOncePerDo_ReusesRecordedErrorWithinPeriod(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	base := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	expectedErr := errors.New("staples not ready")

	guard1, err := NewOncePer(cacheStore, "staples-ready")
	if err != nil {
		t.Fatalf("new onceper: %v", err)
	}
	guard1.now = func() time.Time { return base }
	guard1.claimTTL = time.Second

	guard2, err := NewOncePer(cacheStore, "staples-ready")
	if err != nil {
		t.Fatalf("new onceper: %v", err)
	}
	guard2.now = func() time.Time { return base.Add(10 * time.Minute) }
	guard2.claimTTL = time.Second

	var runs atomic.Int32

	err = guard1.Do(t.Context(), time.Hour, func() error {
		runs.Add(1)
		return expectedErr
	})
	if err == nil || err.Error() != expectedErr.Error() {
		t.Fatalf("expected %q, got %v", expectedErr, err)
	}

	err = guard2.Do(t.Context(), time.Hour, func() error {
		runs.Add(1)
		return nil
	})
	if err == nil || err.Error() != expectedErr.Error() {
		t.Fatalf("expected cached error %q, got %v", expectedErr, err)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("expected cached failure to avoid rerun, got %d executions", got)
	}
}

func TestOncePerDo_ClaimInProgressReturnsErrInProgress(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	base := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)

	guard, err := NewOncePer(cacheStore, "staples-ready")
	if err != nil {
		t.Fatalf("new onceper: %v", err)
	}
	guard.now = func() time.Time { return base }
	guard.claimTTL = time.Second

	if err := cacheStore.Put(t.Context(), guard.claimKey(time.Hour, base), `{"claimed_at":"2026-04-02T12:00:00Z"}`, cache.IfNoneMatch()); err != nil {
		t.Fatalf("seed current claim: %v", err)
	}

	err = guard.Do(t.Context(), time.Hour, func() error {
		return errors.New("claim holder should be the only runner")
	})
	if !errors.Is(err, ErrOncePerInProgress) {
		t.Fatalf("expected in-progress error, got %v", err)
	}
}

func TestOncePerDo_AllowsRetryAfterClaimTTL(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	base := time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)
	now := base

	guard, err := NewOncePer(cacheStore, "staples-ready")
	if err != nil {
		t.Fatalf("new onceper: %v", err)
	}
	guard.now = func() time.Time { return now }
	guard.claimTTL = 20 * time.Millisecond

	stuckClaimKey := guard.claimKey(time.Hour, base)
	if err := cacheStore.Put(t.Context(), stuckClaimKey, `{"claimed_at":"2026-04-02T12:00:00Z"}`, cache.IfNoneMatch()); err != nil {
		t.Fatalf("seed stuck claim: %v", err)
	}

	time.Sleep(guard.claimTTL + 10*time.Millisecond)
	now = base.Add(guard.claimTTL + 10*time.Millisecond)

	if err := guard.Do(t.Context(), time.Hour, func() error { return nil }); err != nil {
		t.Fatalf("expected retry after claim ttl, got %v", err)
	}
}
