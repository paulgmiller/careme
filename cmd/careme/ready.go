package main

import (
	"context"
	"log/slog"
	"net/http"
)

type readyOnce struct {
	done   bool
	checks []Readyable
}

func (r *readyOnce) Ready(ctx context.Context) error {
	if r.done {
		return nil
	}
	for _, check := range r.checks {
		if err := check.Ready(ctx); err != nil {
			return err
		}
	}
	//not thread safe? only ever set to true
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
