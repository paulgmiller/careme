package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"careme/internal/logsetup"
)

type readyOnce struct {
	done   bool
	checks []Readyable
	mu     sync.Mutex
}

func (r *readyOnce) Ready(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil
	}
	ctx = logsetup.WithOperationID(ctx, "readiness_check")
	for _, check := range r.checks {
		if err := check.Ready(ctx); err != nil {
			slog.ErrorContext(ctx, "check failed", "error", err, "check", fmt.Sprintf("%T", check))
			return err
		}
	}
	r.done = true
	return nil
}

type Readyable interface {
	Ready(context.Context) error
}

func (r *readyOnce) Add(f ...Readyable) {
	r.checks = append(r.checks, f...)
}

func (r *readyOnce) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if err := r.Ready(req.Context()); err != nil {
		http.Error(w, "not ready: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if _, err := w.Write([]byte("OK")); err != nil {
		slog.ErrorContext(req.Context(), "failed to write readiness response", "error", err)
	}
}
