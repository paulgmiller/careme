package main

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
)

type logger struct {
	http.Handler
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (l *logger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	//should we use auth client?
	user := ""
	if claims, ok := clerk.SessionClaimsFromContext(r.Context()); ok {
		user = claims.Subject
	}

	lrw := &loggingResponseWriter{w, http.StatusOK}
	l.Handler.ServeHTTP(lrw, r)
	if r.URL.Path == "/ready" {
		return
	}

	slog.Info("request", "method", r.Method, "url", r.URL.Path, "query", r.URL.Query(), "response", lrw.statusCode, "user", user, "form", r.Form, "duration", time.Since(start))
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
