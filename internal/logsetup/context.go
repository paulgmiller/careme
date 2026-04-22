package logsetup

import (
	"context"
	"log/slog"

	"github.com/clerk/clerk-sdk-go/v2"
	"go.opentelemetry.io/otel/trace"
)

type contextKey string

const (
	sessionIDContextKey contextKey = "session_id"
)

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionIDContextKey, sessionID)
}

func SessionIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	sessionID, ok := ctx.Value(sessionIDContextKey).(string)
	if !ok || sessionID == "" {
		return "", false
	}
	return sessionID, true
}

// Cosider https://github.com/PumpkinSeed/slog-context instead
type contextHandler struct {
	handler slog.Handler
}

func newContextHandler(handler slog.Handler) slog.Handler {
	return &contextHandler{handler: handler}
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if sessionID, ok := SessionIDFromContext(ctx); ok {
		record.AddAttrs(slog.String("session_id", sessionID))
	}
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	// hard dependency on clerk is bad. but plumbg an auth
	sessionClaims, ok := clerk.SessionClaimsFromContext(ctx)
	if ok && sessionClaims != nil {
		record.AddAttrs(slog.String("user_id", sessionClaims.Subject))
	}

	return h.handler.Handle(ctx, record)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{handler: h.handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{handler: h.handler.WithGroup(name)}
}
