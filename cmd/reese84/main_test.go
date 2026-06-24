package main

import (
	"testing"
	"time"

	"careme/internal/albertsons"
	"careme/internal/cache"
	"careme/internal/heb"
)

func TestSaveReese84RecordWritesAlbertsonsCompatibleCache(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetchedAt := time.Date(2026, time.June, 5, 12, 0, 0, 0, time.UTC)

	err := saveReese84Record(t.Context(), cacheStore, "albertsons", cookieRecord{
		Cookie:    "cookie-value",
		FetchedAt: fetchedAt,
		SourceURL: defaultSiteURLs["albertsons"],
		Provider:  "test",
	})
	if err != nil {
		t.Fatalf("saveReese84Record returned error: %v", err)
	}

	record, err := albertsons.LoadLatestReese84(t.Context(), cacheStore)
	if err != nil {
		t.Fatalf("LoadLatestReese84 returned error: %v", err)
	}
	if record.Cookie != "cookie-value" {
		t.Fatalf("unexpected cookie: %q", record.Cookie)
	}

	keys, err := cacheStore.List(t.Context(), albertsons.Reese84HistoryPrefix, "")
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

func TestSaveReese84RecordWritesHEBCompatibleCache(t *testing.T) {
	t.Parallel()

	cacheStore := cache.NewInMemoryCache()
	fetchedAt := time.Date(2026, time.June, 5, 13, 0, 0, 0, time.UTC)

	err := saveReese84Record(t.Context(), cacheStore, "heb", cookieRecord{
		Cookie:    "cookie-value",
		FetchedAt: fetchedAt,
		SourceURL: defaultSiteURLs["heb"],
		Provider:  "test",
	})
	if err != nil {
		t.Fatalf("saveReese84Record returned error: %v", err)
	}

	record, err := heb.LoadLatestReese84(t.Context(), cacheStore)
	if err != nil {
		t.Fatalf("LoadLatestReese84 returned error: %v", err)
	}
	if record.Cookie != "cookie-value" {
		t.Fatalf("unexpected cookie: %q", record.Cookie)
	}

	keys, err := cacheStore.List(t.Context(), heb.Reese84HistoryPrefix, "")
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

func TestSaveReese84RecordRejectsInvalidSite(t *testing.T) {
	t.Parallel()

	err := saveReese84Record(t.Context(), cache.NewInMemoryCache(), "../heb", cookieRecord{
		Cookie:    "cookie-value",
		FetchedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
