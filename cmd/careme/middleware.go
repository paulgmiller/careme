package main

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type logger struct {
	http.Handler
}

func (l *logger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	l.Handler.ServeHTTP(w, r)
	if r.URL.Path == "/ready" {
		return
	}
	//TOOO log status code.
	slog.Info("request", "method", r.Method, "url", r.URL.Path, "query", r.URL.Query(), "form", r.Form, "duration", time.Since(start))
}

type recoverer struct {
	http.Handler
}

func (r *recoverer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			slog.ErrorContext(req.Context(), "panic recovered", "error", err, "stack", debug.Stack())
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	}()
	r.Handler.ServeHTTP(w, req)
}

func WithMiddleware(h http.Handler) http.Handler {
	return &logger{
		&recoverer{
			h,
		},
	}
}
