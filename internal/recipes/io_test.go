package recipes

import (
	"careme/internal/cache"
	"careme/internal/locations"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestSaveParams_IsAtomic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "careme-test-saveparams-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	rio := IO(cache.NewFileCache(tmpDir))
	p := DefaultParams(&locations.Location{ID: "123", Name: "Test Store"}, time.Date(2026, 1, 25, 0, 0, 0, 0, time.UTC))

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)

	errs := make(chan error, n)
	for range n {
		go func() {
			defer wg.Done()
			errs <- rio.SaveParams(t.Context(), p)
		}()
	}
	wg.Wait()
	close(errs)

	var ok, alreadyExists, other int
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, AlreadyExists):
			alreadyExists++
		default:
			other++
		}
	}

	if ok != 1 || other != 0 || alreadyExists != n-1 {
		t.Fatalf("expected 1 success + %d AlreadyExists, got ok=%d alreadyExists=%d other=%d", n-1, ok, alreadyExists, other)
	}
}
