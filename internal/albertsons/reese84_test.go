package albertsons

import (
	"context"
	"errors"
	"testing"
	"time"

	"careme/internal/cache"
)

func TestSaveReese84RecordWritesLatestAndHistory(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetchedAt := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)

	err := SaveReese84Record(t.Context(), cacheStore, Reese84Record{
		Cookie:    "cookie-value",
		FetchedAt: fetchedAt,
		SourceURL: "https://www.acmemarkets.com/aisle-vs/meat-seafood/seafood-favorites.html",
		Provider:  brightDataBrowserSource,
		TTLHours:  6,
	})
	if err != nil {
		t.Fatalf("SaveReese84Record returned error: %v", err)
	}

	record, err := LoadLatestReese84(t.Context(), cacheStore)
	if err != nil {
		t.Fatalf("LoadLatestReese84 returned error: %v", err)
	}
	if record.Cookie != "cookie-value" {
		t.Fatalf("unexpected cookie: %q", record.Cookie)
	}

	keys, err := cacheStore.List(t.Context(), Reese84HistoryPrefix, "")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(keys))
	}
	if got, want := keys[0], fetchedAt.Format(time.RFC3339Nano)+".json"; got != want {
		t.Fatalf("unexpected history key: got %q want %q", got, want)
	}
}

func TestLoadFreshReese84RejectsStaleRecord(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetchedAt := time.Date(2026, time.March, 28, 0, 0, 0, 0, time.UTC)
	if err := SaveReese84Record(t.Context(), cacheStore, Reese84Record{
		Cookie:    "cookie-value",
		FetchedAt: fetchedAt,
		SourceURL: "https://www.acmemarkets.com/aisle-vs/meat-seafood/seafood-favorites.html",
		TTLHours:  6,
	}); err != nil {
		t.Fatalf("SaveReese84Record returned error: %v", err)
	}

	_, err := LoadFreshReese84(t.Context(), cacheStore, 6*time.Hour, fetchedAt.Add(7*time.Hour))
	if !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for stale cookie, got %v", err)
	}
}

func TestCachedReese84SourceFallsBackToEnv(t *testing.T) {
	t.Parallel()

	source := NewCachedReese84Source("env-cookie", 6*time.Hour, func() (cache.Cache, error) {
		return cache.NewInMemoryCache(), nil
	})

	got, err := source.Value(context.Background())
	if err != nil {
		t.Fatalf("Value returned error: %v", err)
	}
	if got != "env-cookie" {
		t.Fatalf("unexpected cookie: %q", got)
	}
}
