package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"careme/internal/logsetup"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	sessionCookieName   = "careme_session_id"
	sessionCookieMaxAge = 30 * 60
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
	user := ""
	if claims, ok := clerk.SessionClaimsFromContext(r.Context()); ok {
		user = claims.Subject
	}

	lrw := &loggingResponseWriter{w, http.StatusOK}
	l.Handler.ServeHTTP(lrw, r)

	slog.InfoContext(r.Context(), "request", "method", r.Method, "url", r.URL.Path, "query", r.URL.Query(), "response", lrw.statusCode, "user", user, "form", r.Form, "duration", time.Since(start))
}

type telemetryHandler struct {
	http.Handler
	tracer oteltrace.Tracer
}

func newTelemetryHandler(next http.Handler) http.Handler {
	return &telemetryHandler{
		Handler: next,
		tracer:  otel.Tracer("careme/http"),
	}
}

func (t *telemetryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	spanName := r.URL.Path
	if r.Pattern != "" {
		spanName = r.Pattern
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	ctx, span := t.tracer.Start(ctx, spanName, oteltrace.WithSpanKind(oteltrace.SpanKindServer))
	span.SetAttributes(
		attribute.String("http.method", r.Method),
		attribute.String("http.scheme", scheme),
		attribute.String("http.host", r.Host),
		attribute.String("http.target", r.URL.RequestURI()),
	)

	lrw := &loggingResponseWriter{w, http.StatusOK}
	t.Handler.ServeHTTP(lrw, r.WithContext(ctx))

	span.SetAttributes(attribute.Int("http.status_code", lrw.statusCode))
	if r.Pattern != "" {
		span.SetAttributes(attribute.String("http.route", r.Pattern))
	}
	if lrw.statusCode >= http.StatusBadRequest {
		span.SetStatus(codes.Error, http.StatusText(lrw.statusCode))
	}
	span.End()
}

type recoverer struct {
	http.Handler
}

func (r *recoverer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			span := oteltrace.SpanFromContext(req.Context())
			if span.SpanContext().IsValid() {
				span.RecordError(fmt.Errorf("panic recovered: %v", err))
				span.SetStatus(codes.Error, "panic")
			}
			slog.ErrorContext(req.Context(), "panic recovered", "error", err, "stack", debug.Stack())
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	}()
	r.Handler.ServeHTTP(w, req)
}

type sessionIDHandler struct {
	http.Handler
}

func (h *sessionIDHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := readOrCreateSessionID(r)
	ctx := logsetup.WithSessionID(r.Context(), sessionID)
	http.SetCookie(w, sessionCookie(r, sessionID))
	h.Handler.ServeHTTP(w, r.WithContext(ctx))
}

func readOrCreateSessionID(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return uuid.NewString()
	}
	if _, err := uuid.Parse(cookie.Value); err != nil {
		return uuid.NewString()
	}
	return cookie.Value
}

func sessionCookie(r *http.Request, sessionID string) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   sessionCookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
}

func baseMiddleware(h http.Handler) http.Handler {
	h = &recoverer{h}
	return &logger{h}
}

func appMiddleware(h http.Handler) http.Handler {
	h = baseMiddleware(h)
	h = newTelemetryHandler(h)
	return &sessionIDHandler{h}
}
